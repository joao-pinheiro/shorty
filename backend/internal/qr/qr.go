package qr

import qrcode "github.com/skip2/go-qrcode"

func Generate(url string, size int) ([]byte, error) {
	if size < 128 {
		size = 128
	}
	if size > 1024 {
		size = 1024
	}
	return qrcode.Encode(url, qrcode.Medium, size)
}
