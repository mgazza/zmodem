package zmodem

import (
	"encoding/binary"
	"io"

	"github.com/xiwh/zmodem/myioutil"
)

func (t *ZModem) handleSend() {
	if !t.running.CompareAndSwap(false, true) {
		//避免多个线程一起执行
		return
	}
	defer t.running.CompareAndSwap(true, false)
	for true {
		dataFrame, err := t.readFrame()
		if err != nil {
			//解析错误属于正常现象，因为可能一个大数据包被分成两段发过来了，需要等待第二段到位才能够正常解析
			return
		}
		//println("解析到发送帧")
		//println(dataFrame.ToString() + "\n")
		switch dataFrame.frameType {
		case ZRINIT:
			if t.lastUploadFile != nil {
				//传输完成
				if t.sendFileEOF {
					err = t.sendFrame(newHexFrame(ZFIN, DEFAULT_HEADER_DATA))
					//t.sendFileEOF = false
				}
				break
			}
			zmodemFile := t.consumer.OnUpload()
			if zmodemFile == nil {
				t.close()
				return
			}
			err = t.sendFrame(newBinFrame(ZFILE, DEFAULT_HEADER_DATA))
			if err != nil {
				return
			}
			t.lastUploadFile = zmodemFile
			err = t.sendSubPacket(newSubPacket(ZCRCW, zmodemFile.marshal()), ZBIN, true)
			if zmodemFile == nil {
				t.close()
				return
			}
			break
		case ZRPOS:
			if t.lastUploadFile == nil {
				t.close()
				return
			}

			//发送文件内容
			err = t.sendFrame(newBinFrame(ZDATA, DEFAULT_HEADER_DATA))
			if err != nil {
				t.close()
				return
			}

			size := t.lastUploadFile.Size
			writeCount := 0

			//8k一个包发送
			_, err = myioutil.CopyFixedSize(myioutil.WriteFunc(func(p []byte) (n int, err error) {
				n = len(p)
				isEnd := writeCount+n >= size
				writeCount += n
				if isEnd {
					if err == nil || err == io.EOF {
						//正常读取完毕
						err = t.sendSubPacket(newSubPacket(ZCRCE, p), ZBIN, false)
						if err != nil {
							return
						}
					}
				} else {
					err = t.sendSubPacket(newSubPacket(ZCRCG, p), ZBIN, false)
				}
				return
			}), t.lastUploadFile.Buf, 8096)

			if err != nil && err != io.EOF {
				//非正常关闭
				t.close()
				return
			} else {
				sizeBytes := make([]byte, 4)
				binary.LittleEndian.PutUint32(sizeBytes, uint32(size))
				err = t.sendFrame(newBinFrame(ZEOF, sizeBytes))
				t.sendFileEOF = true
			}
		case ZSKIP:
			//跳过
			if t.lastUploadFile == nil {
				t.close()
				return
			}
			t.consumer.OnUploadSkip(t.lastUploadFile)
			err = t.sendFrame(newHexFrame(ZFIN, DEFAULT_HEADER_DATA))
			_ = t.lastUploadFile.Buf.Close()
			t.lastDownloadFile = nil
		case ZFIN:
			//完成
			_, _ = t.consumer.Writer.Write([]byte{'O', 'O'})
			t.release()
			return
		default:
			t.close()
			return
		}

	}
	return
}
