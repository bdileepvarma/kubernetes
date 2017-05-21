/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aes

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"testing"

	"k8s.io/apiserver/pkg/storage/value"
)

func TestGCMDataStable(t *testing.T) {
	block, err := aes.NewCipher([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	// IMPORTANT: If you must fix this test, then all previously encrypted data from previously compiled versions is broken unless you hardcode the nonce size to 12
	if aead.NonceSize() != 12 {
		t.Fatalf("The underlying Golang crypto size has changed, old version of AES on disk will not be readable unless the AES implementation is changed to hardcode nonce size.")
	}
}

func TestGCMKeyRotation(t *testing.T) {
	testErr := fmt.Errorf("test error")
	block1, err := aes.NewCipher([]byte("abcdefghijklmnop"))
	if err != nil {
		t.Fatal(err)
	}
	block2, err := aes.NewCipher([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}

	context := value.DefaultContext([]byte("authenticated_data"))

	p := value.NewPrefixTransformers(testErr,
		value.PrefixTransformer{Prefix: []byte("first:"), Transformer: NewGCMTransformer(block1)},
		value.PrefixTransformer{Prefix: []byte("second:"), Transformer: NewGCMTransformer(block2)},
	)
	out, err := p.TransformToStorage([]byte("firstvalue"), context)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("first:")) {
		t.Fatalf("unexpected prefix: %q", out)
	}
	from, stale, err := p.TransformFromStorage(out, context)
	if err != nil {
		t.Fatal(err)
	}
	if stale || !bytes.Equal([]byte("firstvalue"), from) {
		t.Fatalf("unexpected data: %t %q", stale, from)
	}

	// verify changing the context fails storage
	from, stale, err = p.TransformFromStorage(out, value.DefaultContext([]byte("incorrect_context")))
	if err == nil {
		t.Fatalf("expected unauthenticated data")
	}

	// reverse the order, use the second key
	p = value.NewPrefixTransformers(testErr,
		value.PrefixTransformer{Prefix: []byte("second:"), Transformer: NewGCMTransformer(block2)},
		value.PrefixTransformer{Prefix: []byte("first:"), Transformer: NewGCMTransformer(block1)},
	)
	from, stale, err = p.TransformFromStorage(out, context)
	if err != nil {
		t.Fatal(err)
	}
	if !stale || !bytes.Equal([]byte("firstvalue"), from) {
		t.Fatalf("unexpected data: %t %q", stale, from)
	}
}

func BenchmarkGCMRead_16_1024(b *testing.B)        { benchmarkGCMRead(b, 16, 1024, false) }
func BenchmarkGCMRead_32_1024(b *testing.B)        { benchmarkGCMRead(b, 32, 1024, false) }
func BenchmarkGCMRead_32_16384(b *testing.B)       { benchmarkGCMRead(b, 32, 16384, false) }
func BenchmarkGCMRead_32_16384_Stale(b *testing.B) { benchmarkGCMRead(b, 32, 16384, true) }

func BenchmarkGCMWrite_16_1024(b *testing.B)  { benchmarkGCMWrite(b, 16, 1024) }
func BenchmarkGCMWrite_32_1024(b *testing.B)  { benchmarkGCMWrite(b, 32, 1024) }
func BenchmarkGCMWrite_32_16384(b *testing.B) { benchmarkGCMWrite(b, 32, 16384) }

func benchmarkGCMRead(b *testing.B, keyLength int, valueLength int, stale bool) {
	block1, err := aes.NewCipher(bytes.Repeat([]byte("a"), keyLength))
	if err != nil {
		b.Fatal(err)
	}
	block2, err := aes.NewCipher(bytes.Repeat([]byte("b"), keyLength))
	if err != nil {
		b.Fatal(err)
	}
	p := value.NewPrefixTransformers(nil,
		value.PrefixTransformer{Prefix: []byte("first:"), Transformer: NewGCMTransformer(block1)},
		value.PrefixTransformer{Prefix: []byte("second:"), Transformer: NewGCMTransformer(block2)},
	)

	context := value.DefaultContext([]byte("authenticated_data"))
	v := bytes.Repeat([]byte("0123456789abcdef"), valueLength/16)

	out, err := p.TransformToStorage(v, context)
	if err != nil {
		b.Fatal(err)
	}
	// reverse the key order if stale
	if stale {
		p = value.NewPrefixTransformers(nil,
			value.PrefixTransformer{Prefix: []byte("first:"), Transformer: NewGCMTransformer(block1)},
			value.PrefixTransformer{Prefix: []byte("second:"), Transformer: NewGCMTransformer(block2)},
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		from, stale, err := p.TransformFromStorage(out, context)
		if err != nil {
			b.Fatal(err)
		}
		if stale {
			b.Fatalf("unexpected data: %t %q", stale, from)
		}
	}
	b.StopTimer()
}

func benchmarkGCMWrite(b *testing.B, keyLength int, valueLength int) {
	block1, err := aes.NewCipher(bytes.Repeat([]byte("a"), keyLength))
	if err != nil {
		b.Fatal(err)
	}
	block2, err := aes.NewCipher(bytes.Repeat([]byte("b"), keyLength))
	if err != nil {
		b.Fatal(err)
	}
	p := value.NewPrefixTransformers(nil,
		value.PrefixTransformer{Prefix: []byte("first:"), Transformer: NewGCMTransformer(block1)},
		value.PrefixTransformer{Prefix: []byte("second:"), Transformer: NewGCMTransformer(block2)},
	)

	context := value.DefaultContext([]byte("authenticated_data"))
	v := bytes.Repeat([]byte("0123456789abcdef"), valueLength/16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.TransformToStorage(v, context)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}