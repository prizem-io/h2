// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"encoding/json"
	"errors"
	"strconv"
)

var (
	// ErrNotFound denotes that a route for a given request path was not found.
	ErrNotFound            = errors.New("route not found")
	ErrServiceUnavailable  = errors.New("service unavailable")
	ErrInternalServerError = errors.New("internal server error")
)

type ErrorResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func respondWithError(stream *Stream, cause error, status int) error {
	data, err := json.Marshal(&ErrorResponse{
		Status:  status,
		Message: cause.Error(),
	})
	if err != nil {
		return err
	}

	err = stream.Connection.SendHeaders(stream, &HeadersParams{
		Headers: Headers{
			{
				Name:  ":status",
				Value: strconv.Itoa(status),
			},
			{
				Name:  "content-type",
				Value: "application/json",
			},
			{
				Name:  "content-length",
				Value: strconv.Itoa(len(data)),
			},
		},
	}, false)
	if err != nil {
		return err
	}

	err = stream.Connection.SendData(stream, data, true)
	if err != nil {
		return err
	}

	return nil
}
