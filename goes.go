// Copyright 2013 Belogik. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package goes provides an API to access Elasticsearch.
package goes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

const (
	// BulkCommandIndex specifies a bulk doc should be indexed
	BulkCommandIndex = "index"
	// BulkCommandDelete specifies a bulk doc should be deleted
	BulkCommandDelete = "delete"
)

func (err *SearchError) Error() string {
	return fmt.Sprintf("[%d] %s", err.StatusCode, err.Msg)
}

// NewClient initiates a new client for an elasticsearch server
//
// This function is pretty useless for now but might be useful in a near future
// if wee need more features like connection pooling or load balancing.
func NewClient(host string, port string) *Client {
	return &Client{host, port, false, http.DefaultClient}
}

func NewHTTPSClient(host string, port string) *Client {
	return &Client{host, port, true, http.DefaultClient}
}

// WithHTTPClient sets the http.Client to be used with the connection. Returns the original client.
func (c *Client) WithHTTPClient(cl *http.Client) *Client {
	c.Client = cl
	return c
}

// CreateIndex creates a new index represented by a name and a mapping
func (c *Client) CreateIndex(name string, mapping interface{}) (*Response, error) {
	r := Request{
		Query:     mapping,
		IndexList: []string{name},
		Method:    "PUT",
	}

	return c.Do(&r)
}

// DeleteIndex deletes an index represented by a name
func (c *Client) DeleteIndex(name string) (*Response, error) {
	r := Request{
		IndexList: []string{name},
		Method:    "DELETE",
	}

	return c.Do(&r)
}

// RefreshIndex refreshes an index represented by a name
func (c *Client) RefreshIndex(name string) (*Response, error) {
	r := Request{
		IndexList: []string{name},
		Method:    "POST",
		API:       "_refresh",
	}

	return c.Do(&r)
}

// UpdateIndexSettings updates settings for existing index represented by a name and a settings
// as described here: https://www.elastic.co/guide/en/elasticsearch/reference/current/indices-update-settings.html
func (c *Client) UpdateIndexSettings(name string, settings interface{}) (*Response, error) {
	r := Request{
		Query:     settings,
		IndexList: []string{name},
		Method:    "PUT",
		API:       "_settings",
	}

	return c.Do(&r)
}

// Optimize an index represented by a name, extra args are also allowed please check:
// http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/indices-optimize.html#indices-optimize
func (c *Client) Optimize(indexList []string, extraArgs url.Values) (*Response, error) {
	r := Request{
		IndexList: indexList,
		ExtraArgs: extraArgs,
		Method:    "POST",
		API:       "_optimize",
	}

	return c.Do(&r)
}

// Stats fetches statistics (_stats) for the current elasticsearch server
func (c *Client) Stats(indexList []string, extraArgs url.Values) (*Response, error) {
	r := Request{
		IndexList: indexList,
		ExtraArgs: extraArgs,
		Method:    "GET",
		API:       "_stats",
	}

	return c.Do(&r)
}

// IndexStatus fetches the status (_status) for the indices defined in
// indexList. Use _all in indexList to get stats for all indices
func (c *Client) IndexStatus(indexList []string) (*Response, error) {
	r := Request{
		IndexList: indexList,
		Method:    "GET",
		API:       "_status",
	}

	return c.Do(&r)
}

// BulkSend bulk adds multiple documents in bulk mode
func (c *Client) BulkSend(documents []Document) (*Response, error) {
	// We do not generate a traditional JSON here (often a one liner)
	// Elasticsearch expects one line of JSON per line (EOL = \n)
	// plus an extra \n at the very end of the document
	//
	// More informations about the Bulk JSON format for Elasticsearch:
	//
	// - http://www.elasticsearch.org/guide/reference/api/bulk.html
	//
	// This is quite annoying for us as we can not use the simple JSON
	// Marshaler available in Run().
	//
	// We have to generate this special JSON by ourselves which leads to
	// the code below.
	//
	// I know it is unreadable I must find an elegant way to fix this.

	// len(documents) * 2 : action + optional_sources
	// + 1 : room for the trailing \n
	bulkData := make([][]byte, len(documents)*2+1)
	i := 0

	for _, doc := range documents {
		action, err := json.Marshal(map[string]interface{}{
			doc.BulkCommand: map[string]interface{}{
				"_index": doc.Index,
				"_type":  doc.Type,
				"_id":    doc.ID,
			},
		})

		if err != nil {
			return &Response{}, err
		}

		bulkData[i] = action
		i++

		if doc.Fields != nil {
			if docFields, ok := doc.Fields.(map[string]interface{}); ok {
				if len(docFields) == 0 {
					continue
				}
			} else {
				typeOfFields := reflect.TypeOf(doc.Fields)
				if typeOfFields.Kind() == reflect.Ptr {
					typeOfFields = typeOfFields.Elem()
				}
				if typeOfFields.Kind() != reflect.Struct {
					return &Response{}, fmt.Errorf("Document fields not in struct or map[string]interface{} format")
				}
				if typeOfFields.NumField() == 0 {
					continue
				}
			}

			sources, err := json.Marshal(doc.Fields)
			if err != nil {
				return &Response{}, err
			}

			bulkData[i] = sources
			i++
		}
	}

	// forces an extra trailing \n absolutely necessary for elasticsearch
	bulkData[len(bulkData)-1] = []byte(nil)

	r := Request{
		Method:   "POST",
		API:      "_bulk",
		BulkData: bytes.Join(bulkData, []byte("\n")),
	}

	resp, err := c.Do(&r)
	if err != nil {
		return resp, err
	}

	if resp.Errors {
		for _, item := range resp.Items {
			for _, i := range item {
				if i.Error != "" {
					return resp, &SearchError{i.Error, i.Status}
				}
			}
		}
		return resp, &SearchError{Msg: "Unknown error while bulk indexing"}
	}

	return resp, err
}

// Search executes a search query against an index
func (c *Client) Search(query interface{}, indexList []string, typeList []string, extraArgs url.Values) (*Response, error) {
	r := Request{
		Query:     query,
		IndexList: indexList,
		TypeList:  typeList,
		Method:    "POST",
		API:       "_search",
		ExtraArgs: extraArgs,
	}

	return c.Do(&r)
}

// Count executes a count query against an index, use the Count field in the response for the result
func (c *Client) Count(query interface{}, indexList []string, typeList []string, extraArgs url.Values) (*Response, error) {
	r := Request{
		Query:     query,
		IndexList: indexList,
		TypeList:  typeList,
		Method:    "POST",
		API:       "_count",
		ExtraArgs: extraArgs,
	}

	return c.Do(&r)
}

//Query runs a query against an index using the provided http method.
//This method can be used to execute a delete by query, just pass in "DELETE"
//for the HTTP method.
func (c *Client) Query(query interface{}, indexList []string, typeList []string, httpMethod string, extraArgs url.Values) (*Response, error) {
	r := Request{
		Query:     query,
		IndexList: indexList,
		TypeList:  typeList,
		Method:    httpMethod,
		API:       "_query",
		ExtraArgs: extraArgs,
	}

	return c.Do(&r)
}

// Scan starts scroll over an index
func (c *Client) Scan(query interface{}, indexList []string, typeList []string, timeout string, size int) (*Response, error) {
	v := url.Values{}
	v.Add("search_type", "scan")
	v.Add("scroll", timeout)
	v.Add("size", strconv.Itoa(size))

	r := Request{
		Query:     query,
		IndexList: indexList,
		TypeList:  typeList,
		Method:    "POST",
		API:       "_search",
		ExtraArgs: v,
	}

	return c.Do(&r)
}

// Scroll fetches data by scroll id
func (c *Client) Scroll(scrollID string, timeout string) (*Response, error) {
	v := url.Values{}
	v.Add("scroll", timeout)

	r := Request{
		Method:    "POST",
		API:       "_search/scroll",
		ExtraArgs: v,
		Body:      []byte(scrollID),
	}

	return c.Do(&r)
}

// Get a typed document by its id
func (c *Client) Get(index string, documentType string, id string, extraArgs url.Values) (*Response, error) {
	r := Request{
		IndexList: []string{index},
		Method:    "GET",
		API:       documentType + "/" + id,
		ExtraArgs: extraArgs,
	}

	return c.Do(&r)
}

// Index indexes a Document
// The extraArgs is a list of url.Values that you can send to elasticsearch as
// URL arguments, for example, to control routing, ttl, version, op_type, etc.
func (c *Client) Index(d Document, extraArgs url.Values) (*Response, error) {
	r := Request{
		Query:     d.Fields,
		IndexList: []string{d.Index.(string)},
		TypeList:  []string{d.Type},
		ExtraArgs: extraArgs,
		Method:    "POST",
	}

	if d.ID != nil {
		r.Method = "PUT"
		r.ID = d.ID.(string)
	}

	return c.Do(&r)
}

// Delete deletes a Document d
// The extraArgs is a list of url.Values that you can send to elasticsearch as
// URL arguments, for example, to control routing.
func (c *Client) Delete(d Document, extraArgs url.Values) (*Response, error) {
	r := Request{
		IndexList: []string{d.Index.(string)},
		TypeList:  []string{d.Type},
		ExtraArgs: extraArgs,
		Method:    "DELETE",
		ID:        d.ID.(string),
	}

	return c.Do(&r)
}

// Buckets returns list of buckets in aggregation
func (a Aggregation) Buckets() []Bucket {
	result := []Bucket{}
	if buckets, ok := a["buckets"]; ok {
		for _, bucket := range buckets.([]interface{}) {
			result = append(result, bucket.(map[string]interface{}))
		}
	}

	return result
}

// Key returns key for aggregation bucket
func (b Bucket) Key() interface{} {
	return b["key"]
}

// DocCount returns count of documents in this bucket
func (b Bucket) DocCount() uint64 {
	return uint64(b["doc_count"].(float64))
}

// Aggregation returns aggregation by name from bucket
func (b Bucket) Aggregation(name string) Aggregation {
	if agg, ok := b[name]; ok {
		return agg.(map[string]interface{})
	}
	return Aggregation{}
}

// PutMapping registers a specific mapping for one or more types in one or more indexes
func (c *Client) PutMapping(typeName string, mapping interface{}, indexes []string) (*Response, error) {

	r := Request{
		Query:     mapping,
		IndexList: indexes,
		Method:    "PUT",
		API:       "_mappings/" + typeName,
	}

	return c.Do(&r)
}

// GetMapping returns the mappings for the specified types
func (c *Client) GetMapping(types []string, indexes []string) (*Response, error) {

	r := Request{
		IndexList: indexes,
		Method:    "GET",
		API:       "_mapping/" + strings.Join(types, ","),
	}

	return c.Do(&r)
}

// IndicesExist checks whether index (or indices) exist on the server
func (c *Client) IndicesExist(indexes []string) (bool, error) {

	r := Request{
		IndexList: indexes,
		Method:    "HEAD",
	}

	resp, err := c.Do(&r)

	return resp.Status == 200, err
}

// Update updates the specified document using the _update endpoint
func (c *Client) Update(d Document, query interface{}, extraArgs url.Values) (*Response, error) {
	r := Request{
		Query:     query,
		IndexList: []string{d.Index.(string)},
		TypeList:  []string{d.Type},
		ExtraArgs: extraArgs,
		Method:    "POST",
		API:       "_update",
	}

	if d.ID != nil {
		r.ID = d.ID.(string)
	}

	return c.Do(&r)
}

// DeleteMapping deletes a mapping along with all data in the type
func (c *Client) DeleteMapping(typeName string, indexes []string) (*Response, error) {

	r := Request{
		IndexList: indexes,
		Method:    "DELETE",
		API:       "_mappings/" + typeName,
	}

	return c.Do(&r)
}

func (c *Client) modifyAlias(action string, alias string, indexes []string) (*Response, error) {
	command := map[string]interface{}{
		"actions": make([]map[string]interface{}, 1),
	}

	for _, index := range indexes {
		command["actions"] = append(command["actions"].([]map[string]interface{}), map[string]interface{}{
			action: map[string]interface{}{
				"index": index,
				"alias": alias,
			},
		})
	}

	r := Request{
		Query:  command,
		Method: "POST",
		API:    "_aliases",
	}

	return c.Do(&r)
}

// AddAlias creates an alias to one or more indexes
func (c *Client) AddAlias(alias string, indexes []string) (*Response, error) {
	return c.modifyAlias("add", alias, indexes)
}

// RemoveAlias removes an alias to one or more indexes
func (c *Client) RemoveAlias(alias string, indexes []string) (*Response, error) {
	return c.modifyAlias("remove", alias, indexes)
}

// AliasExists checks whether alias is defined on the server
func (c *Client) AliasExists(alias string) (bool, error) {

	r := Request{
		Method: "HEAD",
		API:    "_alias/" + alias,
	}

	resp, err := c.Do(&r)

	return resp.Status == 200, err
}

// Do runs the request returned by the requestor and returns the parsed response
func (c *Client) Do(r Requester) (*Response, error) {
	req, err := r.Request()
	if err != nil {
		return &Response{}, err
	}
	if c.IsHTTPS {
		req.URL.Scheme = "https"
	} else {
		req.URL.Scheme = "http"
	}
	req.URL.Host = fmt.Sprintf("%s:%s", c.Host, c.Port)

	body, statusCode, err := c.doRequest(req)
	esResp := &Response{Status: statusCode}

	if err != nil {
		return esResp, err
	}

	if req.Method != "HEAD" {
		err = json.Unmarshal(body, &esResp)
		if err != nil {
			return esResp, err
		}
		err = json.Unmarshal(body, &esResp.Raw)
		if err != nil {
			return esResp, err
		}
	}

	if len(esResp.RawError) > 0 && esResp.RawError[0] == '"' {
		json.Unmarshal(esResp.RawError, &esResp.Error)
	} else {
		esResp.Error = string(esResp.RawError)
	}
	esResp.RawError = nil

	if esResp.Error != "" {
		return esResp, &SearchError{esResp.Error, esResp.Status}
	}

	return esResp, nil
}

func (c *Client) doRequest(req *http.Request) ([]byte, uint64, error) {
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, uint64(resp.StatusCode), err
	}

	if resp.StatusCode > 201 && resp.StatusCode < 400 {
		return nil, uint64(resp.StatusCode), errors.New(string(body))
	}

	return body, uint64(resp.StatusCode), nil
}
