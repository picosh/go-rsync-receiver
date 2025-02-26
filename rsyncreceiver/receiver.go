package rsyncreceiver

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"log"

	"github.com/mmcloughlin/md4"
	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/utils"
)

// rsync/receiver.c:recv_files
func (rt *Transfer) RecvFiles(fileList []*utils.ReceiverFile) error {
	phase := 0
	for {
		idx, err := rt.Conn.ReadInt32()
		if err != nil {
			return err
		}
		if idx == -1 {
			if phase == 0 {
				phase++
				log.Printf("recvFiles phase=%d", phase)
				// TODO: send done message
				continue
			}
			break
		}
		log.Printf("receiving file idx=%d: %+v", idx, fileList[idx])
		if err := rt.recvFile1(fileList[idx]); err != nil {
			return err
		}
	}
	log.Printf("recvFiles finished")
	return nil
}

func (rt *Transfer) recvFile1(f *utils.ReceiverFile) error {
	if rt.Opts.DryRun {
		fmt.Println(f.Name)
		return nil
	}

	localFile, err := rt.openLocalFile(f)
	if err != nil {
		log.Printf("opening local file failed, continuing: %v", err)
	} else {
		defer localFile.Close()
	}
	if err := rt.receiveData(f, localFile); err != nil {
		return err
	}
	return nil
}

func (rt *Transfer) openLocalFile(f *utils.ReceiverFile) (utils.ReaderAtCloser, error) {
	_, r, err := rt.files.Read(&utils.SenderFile{
		Path:    f.Name,
		Regular: true,
	})

	if err != nil {
		return nil, err
	}

	return r, nil
}

// rsync/receiver.c:receive_data
func (rt *Transfer) receiveData(f *utils.ReceiverFile, localFile utils.ReaderAtCloser) error {
	var sh rsync.SumHead
	if err := sh.ReadFrom(rt.Conn); err != nil {
		return err
	}

	r, w := io.Pipe()

	f.Reader = r

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer func() {
			wg.Done()
			if err := r.Close(); err != nil {
				return
			}
		}()

		_, err := rt.files.Put(f)
		if err != nil {
			return
		}
	}()

	h := md4.New()
	binary.Write(h, binary.LittleEndian, rt.Seed)

	wr := io.MultiWriter(w, h)

	for {
		token, data, err := rt.recvToken()
		if err != nil {
			return err
		}
		if token == 0 {
			break
		}
		if token > 0 {
			if _, err := wr.Write(data); err != nil {
				return err
			}
			continue
		}
		if localFile == nil {
			return fmt.Errorf("BUG: local file %s not open for copying chunk", localFile)
		}
		token = -(token + 1)
		offset2 := int64(token) * int64(sh.BlockLength)
		dataLen := sh.BlockLength
		if token == sh.ChecksumCount-1 && sh.RemainderLength != 0 {
			dataLen = sh.RemainderLength
		}
		data = make([]byte, dataLen)
		if _, err := localFile.ReadAt(data, offset2); err != nil {
			return err
		}

		if _, err := wr.Write(data); err != nil {
			return err
		}
	}

	if err := w.Close(); err != nil {
		return err
	}

	wg.Wait()

	localSum := h.Sum(nil)
	remoteSum := make([]byte, len(localSum))
	if _, err := io.ReadFull(rt.Conn.Reader, remoteSum); err != nil {
		return err
	}
	if !bytes.Equal(localSum, remoteSum) {
		return fmt.Errorf("file corruption in %s", f.Name)
	}
	log.Printf("checksum %x matches!", localSum)

	return nil
}
