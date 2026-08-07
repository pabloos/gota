// Package repository declares the Signature returned by the api handler.
package repository

type Signature struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
