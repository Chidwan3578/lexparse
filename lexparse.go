// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package lexparse defines a set of interfaces that can be used to define
// generic lexers and parsers over byte streams.
package lexparse

import (
	"context"
	"errors"
	"io"
	"sync"
)

// channelBufSize is the size of the buffer for the token channel used between
// the lexer and parser.
const channelBufSize = 1024

// tokenChan implements the [TokenSource] interface by reading tokens from
// a channel.
type tokenChan struct {
	c chan *Token
}

// NextToken implements [TokenSource.NextToken].
func (tc *tokenChan) NextToken(_ context.Context) *Token {
	// NOTE: We do not check if the Context is done. The same context is used
	//       for the lexer and the lexer should return an EOF token after the
	//       Context is canceled. It is important to return the EOF token from
	//       the lexer rather than return our own EOF token here to capture the
	//       Position values of the EOF token.
	return <-tc.c
}

// LexParse lexes the content the given lexer and feeds the tokens concurrently
// to the parser starting at startingState. The resulting root node of the parse
// tree is returned.
func LexParse[V comparable](
	ctx context.Context,
	lex Lexer,
	startingState ParseState[V],
) (*Node[V], error) {
	var (
		root     *Node[V]
		lexErr   error
		parseErr error
		waitGrp  sync.WaitGroup
	)

	ctx, cancel := context.WithCancel(ctx)

	tokens := &tokenChan{
		c: make(chan *Token, channelBufSize),
	}

	p := NewParser(tokens, startingState)

	waitGrp.Add(1)

	go func() {
		t := &Token{}
		for t.Type != TokenTypeEOF {
			t = lex.NextToken(ctx)
			tokens.c <- t
		}

		lexErr = lex.Err()

		waitGrp.Done()
	}()

	waitGrp.Add(1)

	go func() {
		root, parseErr = p.Parse(ctx)

		cancel() // Indicate that parsing is done.
		waitGrp.Done()
	}()

	waitGrp.Wait()

	err := lexErr
	// Do not report context.Canceled errors from the Lexer. If the context is
	// canceled by the caller the parser will also return this error.
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		err = parseErr
	}

	return root, err
}
