package godownloader

import (
	"math/rand"
	"strings"
	"time"
)

const TypeDownloadHTTP = "HTTP"
const TypeDownloadTorrent = "TORRENT"

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func init() {
	rand.Seed(time.Now().UnixNano())
}
func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func IsMagnet(url string) bool {
	return strings.HasPrefix(url, "magnet")
}
