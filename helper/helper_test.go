package helper

import "testing"

func TestHideURL(t *testing.T) {
	urls := []string{
		"https://www.baidu.com",
		"http://www.baidu.com",
	}
	for _, url := range urls {
		t.Log(HideURL(url))
	}
}
