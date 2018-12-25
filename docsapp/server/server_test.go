package main

import (
	"net/http"
	"testing"
)

func BenchmarkGetDocsHandler(b *testing.B) {
	b.StopTimer()
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://localhost:8080/docs", nil)
	if err != nil {
		return
	}
	req.URL.RawQuery = "key=&limit=&login=&token=143136bc-4439-42f9-9e5b-21303849e14d&value="
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		client.Do(req)
	}
}
