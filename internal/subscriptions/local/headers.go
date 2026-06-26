package local

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func ClientHeaders(client *model.Client, updateInterval int) []string {
	var headers []string
	headers = append(headers, fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", client.Up, client.Down, client.Volume, client.Expiry))
	headers = append(headers, fmt.Sprintf("%d", updateInterval))
	headers = append(headers, client.Name)
	return headers
}
