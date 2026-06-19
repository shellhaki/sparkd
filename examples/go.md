# Go

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func main() {
	body := []byte(`{"name":"go-cell","dbtype":"pg","port":3001,"ram":"128mb","disk":"512mb"}`)

	resp, err := http.Post("http://server-ip:8721/create", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.Status)
	fmt.Println(string(data))
}
```
