package godownloader

//Code from https://github.com/t3rm1n4l/godownload

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	u "net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	NotStarted = "Not started"
	OnProgress = "On progress"
	Completed  = "Completed"
)

type Status string

type HTTPDownloader struct {
	url         string
	conns       int
	file        *os.File
	size        int64
	parts       []Part
	start       time.Time
	end         time.Time
	done        chan error
	quit        chan bool
	status      string
	dlpath      string
	name        string
	isCancelled bool
}

func NewHTTPDownloader() HTTPDownloader {
	return HTTPDownloader{}
}

func (dl *HTTPDownloader) SniffFilename(url string, header http.Header) string {
	var filename string
	var err error
	_, params, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	filename = params["filename"]
	if filename == "" {
		d := strings.Split(url, "/")
		filename, err = u.QueryUnescape(d[len(d)-1])
		if err != nil {
			filename = d[len(d)-1]
		}
	}
	return filename
}

func (dl *HTTPDownloader) GetFileName() string {
	return dl.name
}

func (dl *HTTPDownloader) SniffMimeType(header http.Header) string {
	var mimetype string
	mimetype = header.Get("Content-Type")
	if mimetype == "" {
		mimetype = "text/plain"
	}
	return mimetype
}

func (dl *HTTPDownloader) GetPath() string {
	return dl.dlpath
}

func (dl *HTTPDownloader) Init(url string, conns int, dir string, filename string) (string, uint64, error) {
	var mimetype string
	dl.url = url
	dl.conns = conns
	dl.status = NotStarted
	resp, err := http.Head(url)
	if err != nil {
		return mimetype, 0, err
	}
	if resp.StatusCode != 200 {
		return mimetype, 0, errors.New(resp.Status)
	}
	fmt.Println(resp.Header)
	mimetype = dl.SniffMimeType(resp.Header)
	filename = dl.SniffFilename(url, resp.Header)
	dl.name = filename
	os.MkdirAll(dir, 0755)
	filename = path.Join(dir, filename)
	dl.dlpath = filename

	dl.size, err = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)

	if err != nil {
		return mimetype, 0, errors.New("Not supported for download")
	}
	if dl.size < 4096*1024 {
		dl.conns = 1
	}
	_, err = os.Stat(filename)
	if os.IsExist(err) {
		os.Remove(filename)
	}

	dl.file, err = os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return mimetype, 0, err
	}

	dl.parts = make([]Part, dl.conns)
	size := dl.size / int64(dl.conns)

	for i, part := range dl.parts {
		part.id = i
		part.url = dl.url
		part.offset = i * int(size)
		switch {
		case i == dl.conns-1:
			part.size = dl.size - size*int64(i)
			break
		default:
			part.size = size
		}
		part.dlsize = 0
		part.file = dl.file
		dl.parts[i] = part
	}

	return mimetype, uint64(dl.size), nil
}

func (dl *HTTPDownloader) StartDownload() {
	dl.done = make(chan error, dl.conns)
	dl.quit = make(chan bool, dl.conns)
	dl.status = OnProgress
	for i := 0; i < dl.conns; i++ {
		go dl.parts[i].Download(dl.done, dl.quit)
	}
	dl.start = time.Now()
}

func (dl *HTTPDownloader) CancelDownload() {
	dl.quit <- true
}

type ProgressStatus struct {
	Status     string
	Total      int64
	Downloaded int64
	Elapsed    time.Duration
}

func NewProgressStatus(status string, total, downloaded int64, elapsed time.Duration) *ProgressStatus {
	return &ProgressStatus{
		Status:     status,
		Total:      total,
		Downloaded: downloaded,
		Elapsed:    elapsed,
	}
}

func (dl HTTPDownloader) GetProgress() *ProgressStatus {
	var dlsize int64
	for _, part := range dl.parts {
		dlsize += part.dlsize
	}
	return NewProgressStatus(dl.status, dl.size, dlsize, time.Now().Sub(dl.start))
}

func (dl *HTTPDownloader) Wait() error {
	var err error = nil
	for i := 0; i < dl.conns; i++ {
		e := <-dl.done
		if e != nil {
			err = e
			dl.status = err.Error()
			for j := i; j < dl.conns; j++ {
				dl.quit <- true
			}
		}
	}
	close(dl.done)
	dl.end = time.Now()
	dl.file.Close()
	if dl.status == OnProgress {
		dl.status = Completed
	}
	return err
}

func (dl *HTTPDownloader) Download() error {
	dl.StartDownload()
	return dl.Wait()
}

type Part struct {
	id     int
	url    string
	offset int
	dlsize int64
	size   int64
	file   *os.File
}

func (part *Part) Download(done chan error, quit chan bool) error {
	client := http.Client{}
	var size int64 = 4096
	buffer := make([]byte, size)
	req, err := http.NewRequest("GET", part.url, nil)
	defer func() {
		done <- err
	}()

	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", part.offset, int64(part.offset)+part.size-1))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for {
		select {
		case <-quit:
			return nil
		default:
		}

		nbytes, err := resp.Body.Read(buffer[0:size])
		if err != nil && err != io.EOF { // don't break the loop on read EOF
			return err
		}

		nbytes, err = part.file.WriteAt(buffer[0:nbytes], int64(part.offset)+part.dlsize)
		if err != nil {
			return nil
		}
		part.dlsize += int64(nbytes)
		remaining := part.size - part.dlsize
		switch {
		case remaining == 0:
			return nil
		case remaining < 4096:
			size = part.size - part.dlsize
		}
	}

	return nil
}
