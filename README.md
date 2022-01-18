# Go-Downloader


Example:-

```
package main

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	godownloader "github.com/jaskaranSM/go-downloader"
)

type DlListener struct {
}

func (dl *DlListener) OnDownloadStart(gid string, dlinfo *godownloader.DownloadInfo) {
	fmt.Printf("[OnDownloadStart]: %s\n", gid)
}

func (dl *DlListener) OnDownloadStop(gid string, dlinfo *godownloader.DownloadInfo) {
	fmt.Printf("[OnDownloadStop]: %s\n", gid)
	fmt.Printf("Error: %s\n", dlinfo.Error.Error())
}

func (dl *DlListener) OnDownloadComplete(gid string, dlinfo *godownloader.DownloadInfo) {
	fmt.Printf("[OnDownloadComplete]: %s\n", gid)
}

func (dl *DlListener) OnDownloadProgress(gid string, dlinfo *godownloader.DownloadInfo) {
	fmt.Printf("%s, Speed: %s, Downloaded: %s, Total: %s, Type: %s\n",
		dlinfo.Name,
		humanize.Bytes(uint64(dlinfo.Speed)),
		humanize.Bytes(uint64(dlinfo.CompletedLength)),
		humanize.Bytes(uint64(dlinfo.TotalLength)),
		dlinfo.Type)
}

func main() {
	engine := godownloader.NewDownloadEngine()
	var options map[string]string = make(map[string]string)
	options["connections"] = "16"
	options["dir"] = "dls"
	listener := &DlListener{}
	engine.AddEventListener(listener)
	gid := engine.AddURL("magnet:?xt=urn:btih:adcb42c09f980a746f9df41f2d8d05e862eeac55&dn=ubuntu-mate-20.04.3-desktop-amd64.iso&tr=https%3A%2F%2Ftorrent.ubuntu.com%2Fannounce", options)
	fmt.Println(gid)
	for {
		time.Sleep(1 * time.Second)
	}
}

```