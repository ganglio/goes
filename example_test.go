// Copyright 2013 Belogik. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goes_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ganglio/goes"
)

var (
	ES_HOST = "localhost"
	ES_PORT = "9200"
)

func getClient() (conn *goes.Client) {
	h := os.Getenv("TEST_ELASTICSEARCH_HOST")
	if h == "" {
		h = ES_HOST
	}

	p := os.Getenv("TEST_ELASTICSEARCH_PORT")
	if p == "" {
		p = ES_PORT
	}

	conn = goes.NewClient(h, p)

	return
}

func ExampleClient_CreateIndex() {
	conn := getClient()

	mapping := map[string]interface{}{
		"settings": map[string]interface{}{
			"index.number_of_shards":   1,
			"index.number_of_replicas": 0,
		},
		"mappings": map[string]interface{}{
			"_default_": map[string]interface{}{
				"_source": map[string]interface{}{
					"enabled": true,
				},
				"_all": map[string]interface{}{
					"enabled": false,
				},
			},
		},
	}

	resp, err := conn.CreateIndex("test", mapping)

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", resp)
}

func ExampleClient_DeleteIndex() {
	conn := getClient()
	resp, err := conn.DeleteIndex("yourinde")

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", resp)
}

func ExampleClient_RefreshIndex() {
	conn := getClient()
	resp, err := conn.RefreshIndex("yourindex")

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", resp)
}

func ExampleClient_Search() {
	conn := getClient()

	var query = map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": map[string]interface{}{
					"match_all": map[string]interface{}{},
				},
			},
		},
		"from":   0,
		"size":   100,
		"fields": []string{"onefield"},
		"filter": map[string]interface{}{
			"range": map[string]interface{}{
				"somefield": map[string]interface{}{
					"from":          "some date",
					"to":            "some date",
					"include_lower": false,
					"include_upper": false,
				},
			},
		},
	}

	extraArgs := make(url.Values, 1)

	searchResults, err := conn.Search(query, []string{"someindex"}, []string{""}, extraArgs)

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", searchResults)
}

func ExampleClient_Index() {
	conn := getClient()

	d := goes.Document{
		Index: "twitter",
		Type:  "tweet",
		Fields: map[string]interface{}{
			"user":    "foo",
			"message": "bar",
		},
	}

	extraArgs := make(url.Values, 1)
	extraArgs.Set("ttl", "86400000")

	response, err := conn.Index(d, extraArgs)

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", response)
}

func ExampleClient_Delete() {
	conn := getClient()

	//[create index, index document ...]

	d := goes.Document{
		Index: "twitter",
		Type:  "tweet",
		ID:    "1",
		Fields: map[string]interface{}{
			"user": "foo",
		},
	}

	response, err := conn.Delete(d, url.Values{})
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", response)
}

func ExampleClient_WithHTTPClient() {
	tr := &http.Transport{
		ResponseHeaderTimeout: 1 * time.Second,
	}
	cl := &http.Client{
		Transport: tr,
	}
	conn := getClient()
	conn.WithHTTPClient(cl)

	fmt.Printf("%v\n", conn.Client)
}
