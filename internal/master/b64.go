package master

import "encoding/base64"

func stdB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
