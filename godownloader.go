package godownloader

import (
	"fmt"
	"path"
	"strconv"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	EventStart    int = 0
	EventComplete int = 1
	EventStop     int = 2
	EventProgress int = 3
)

type DownloadListener interface {
	OnDownloadStart(string, *DownloadInfo)
	OnDownloadComplete(string, *DownloadInfo)
	OnDownloadProgress(string, *DownloadInfo)
	OnDownloadStop(string, *DownloadInfo)
}

type DownloadRequest struct {
	URL     string
	Options map[string]string
	Gid     string
}

type DownloadInfo struct {
	Type                string
	Gid                 string
	TotalLength         int64
	CompletedLength     int64
	Speed               int64
	ETA                 time.Duration
	DlPath              string
	Dir                 string
	MimeType            string
	Dler                *HTTPDownloader
	Error               error
	IsComplete          bool
	IsFailed            bool
	IsCancelled         bool
	CancellationChannel chan bool
	Torrent             *torrent.Torrent
	TorrentClient       *torrent.Client
	Name                string
	IsMetadata          bool
}

func NewDownloadInfo() *DownloadInfo {
	return &DownloadInfo{}
}

func NewDownloadEngine() *DownloadEngine {
	engine := &DownloadEngine{
		dls:      make(map[string]*DownloadInfo),
		receiver: make(chan *DownloadRequest),
	}
	go engine.Listener()
	return engine
}

type DownloadEngine struct {
	dls       map[string]*DownloadInfo
	receiver  chan *DownloadRequest
	Listeners []DownloadListener
}

func (d *DownloadEngine) SendDownloadRequest(dr *DownloadRequest) {
	d.receiver <- dr
}

func (d *DownloadEngine) AddDownloadInfoByGid(gid string, dlinfo *DownloadInfo) {
	d.dls[gid] = dlinfo
}

func (d *DownloadEngine) CancelDownloadByGid(gid string) {
	dlinfo := d.GetDownloadInfoByGid(gid)
	if dlinfo == nil {
		return
	}
	dlinfo.IsFailed = true
	dlinfo.Error = fmt.Errorf("Cancelled by user.")
	dlinfo.IsCancelled = true
	if dlinfo.Type == TypeDownloadHTTP {
		dlinfo.Dler.CancelDownload()
	} else {
		dlinfo.Torrent.Drop()
	}
	dlinfo.CancellationChannel <- true
	d.NotifyEvent(EventStop, gid)
}

func (d *DownloadEngine) AddEventListener(listener DownloadListener) {
	d.Listeners = append(d.Listeners, listener)
}

func (d *DownloadEngine) NotifyEvent(event int, gid string) {
	dlinfo := d.GetDownloadInfoByGid(gid)
	for _, listener := range d.Listeners {
		switch event {
		case EventStart:
			listener.OnDownloadStart(gid, dlinfo)
		case EventComplete:
			listener.OnDownloadComplete(gid, dlinfo)
		case EventStop:
			listener.OnDownloadStop(gid, dlinfo)
		case EventProgress:
			listener.OnDownloadProgress(gid, dlinfo)
		}
	}
}

func (d *DownloadEngine) HandleDownloadRequest(dr *DownloadRequest) string {
	dlinfo := NewDownloadInfo()
	var connections int = 1
	var dir string = "."
	var gid string = RandStringRunes(6)
	dr.Gid = gid
	if val, ok := dr.Options["connections"]; ok {
		connections, _ = strconv.Atoi(val)
	}
	if val, ok := dr.Options["dir"]; ok {
		dir = val
	}
	dlinfo.Dir = dir
	dlinfo.Gid = gid
	if IsMagnet(dr.URL) {
		dlinfo.Type = "TORRENT"
		config := torrent.NewDefaultClientConfig()
		config.DataDir = dlinfo.Dir
		d.AddDownloadInfoByGid(gid, dlinfo)
		d.NotifyEvent(EventStart, gid)
		dlinfo.TorrentClient, _ = torrent.NewClient(config)
		t, err := dlinfo.TorrentClient.AddMagnet(dr.URL)
		if err != nil {
			dlinfo.Error = err
			dlinfo.IsComplete = false
			dlinfo.IsFailed = true
			d.NotifyEvent(EventStop, dlinfo.Gid)
			return gid
		}
		dlinfo.Torrent = t
		dlinfo.IsMetadata = true
		fmt.Println(dlinfo.Type)
		go func() {
			go d.MonitorTorrentProgress(t, dlinfo)
			select {
			case <-t.GotInfo():
				fmt.Println("Got metadata")
				dlinfo.IsMetadata = false
				t.DownloadAll()
				dlinfo.DlPath = path.Join(dlinfo.Dir, t.Info().Name)
			case <-dlinfo.CancellationChannel:
			}
		}()
		return gid
	}
	dler := NewHTTPDownloader()
	mime, size, err := dler.Init(dr.URL, connections, dir, "")
	dlinfo.TotalLength = int64(size)
	dlinfo.Dler = &dler
	dlinfo.Type = "HTTP"
	dlinfo.DlPath = dler.GetPath()
	dlinfo.MimeType = mime
	d.AddDownloadInfoByGid(gid, dlinfo)
	d.NotifyEvent(EventStart, gid)
	if err != nil {
		dlinfo.Error = err
		dlinfo.IsFailed = true
		dlinfo.IsComplete = false
		d.NotifyEvent(EventStop, gid)
		return gid
	}
	go func() {
		dler.StartDownload()
		go d.MonitorHTTPProgress(&dler, dlinfo)
		dler.Wait()
		fmt.Println(mime)
		if dlinfo.MimeType == "application/x-bittorrent" {
			d.HandleTorrentDownload(&dler, dlinfo)
		}
	}()
	return gid
}

func (d *DownloadEngine) HandleTorrentDownload(dler *HTTPDownloader, dlinfo *DownloadInfo) {
	config := torrent.NewDefaultClientConfig()
	config.DataDir = dlinfo.Dir
	dlinfo.TorrentClient, _ = torrent.NewClient(config)
	t, err := dlinfo.TorrentClient.AddTorrentFromFile(dler.GetPath())
	if err != nil {
		dlinfo.Error = err
		dlinfo.IsComplete = false
		dlinfo.IsFailed = true
		d.NotifyEvent(EventStop, dlinfo.Gid)
		return
	}
	dlinfo.Type = "TORRENT"
	dlinfo.Torrent = t
	dlinfo.IsMetadata = true
	fmt.Println(dlinfo.Type)
	go d.MonitorTorrentProgress(t, dlinfo)
	select {
	case <-t.GotInfo():
		fmt.Println("Got metadata")
		dlinfo.IsMetadata = false
		t.DownloadAll()
	case <-dlinfo.CancellationChannel:
	}
}

func (d *DownloadEngine) MonitorTorrentProgress(t *torrent.Torrent, dlinfo *DownloadInfo) {
	startTime := time.Now()
	var completed int64
	var speed int64
	var total int64
	for !t.Complete.Bool() {
		if dlinfo.IsCancelled {
			return
		}
		if dlinfo.IsMetadata {
			continue
		}
		dlinfo.Name = t.Info().Name
		total = t.Info().TotalLength()
		stats := t.Stats()
		completed = stats.BytesReadData.Int64()
		if int64(time.Now().Sub(startTime).Seconds()) != 0 {
			speed = completed / int64(time.Now().Sub(startTime).Seconds())
		}
		dlinfo.CompletedLength = completed
		dlinfo.Speed = speed
		dlinfo.TotalLength = total
		dlinfo.ETA = CalculateETA(dlinfo.TotalLength-dlinfo.CompletedLength, dlinfo.Speed)
		d.NotifyEvent(EventProgress, dlinfo.Gid)
	}
	dlinfo.IsComplete = true
	dlinfo.IsFailed = false
	d.NotifyEvent(EventComplete, dlinfo.Gid)
}

func (d *DownloadEngine) MonitorHTTPProgress(dler *HTTPDownloader, dlinfo *DownloadInfo) {
	for {
		if dlinfo.IsCancelled {
			return
		}
		dlinfo.Name = dler.GetFileName()
		progress := dler.GetProgress()
		if progress.Status == Completed {
			dlinfo.IsComplete = true
			dlinfo.IsFailed = false
			break
		}
		dlinfo.CompletedLength = int64(progress.Downloaded)
		dlinfo.TotalLength = int64(progress.Total)
		var speed int64 = 0
		if int64(progress.Elapsed.Seconds()) != 0 {
			speed = progress.Downloaded / int64(progress.Elapsed.Seconds())
		}
		dlinfo.Speed = int64(speed)
		dlinfo.ETA = CalculateETA(dlinfo.TotalLength-dlinfo.CompletedLength, dlinfo.Speed)
		d.NotifyEvent(EventProgress, dlinfo.Gid)
	}
	if dlinfo.MimeType != "application/x-bittorrent" {
		dlinfo.IsComplete = true
		d.NotifyEvent(EventComplete, dlinfo.Gid)
	}
}

func (d *DownloadEngine) Listener() {
	for dr := range d.receiver {
		go d.HandleDownloadRequest(dr)
	}
}

func (d *DownloadEngine) GetDownloadInfoByGid(gid string) *DownloadInfo {
	for i, dl := range d.dls {
		if i == gid {
			return dl
		}
	}
	return nil
}

func (d *DownloadEngine) AddURL(url string, options map[string]string) string {
	dr := &DownloadRequest{
		URL:     url,
		Options: options,
	}
	return d.HandleDownloadRequest(dr)
}
