package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeHTTP(t *testing.T) {
	p := &Plugin{}
	p.router = p.initRouter()
	assert.NotNil(t, p.router)
}
