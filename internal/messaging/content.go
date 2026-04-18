package messaging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	contentKindText       = "text"
	contentKindPhotoChunk = "photo-chunk"
	photoChunkSizeBytes   = 8 * 1024
)

type contentPayload struct {
	Kind       string             `json:"kind"`
	Text       string             `json:"text,omitempty"`
	PhotoChunk *photoChunkPayload `json:"photo_chunk,omitempty"`
}

type photoChunkPayload struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	MIMEType     string `json:"mime_type"`
	TotalSize    int    `json:"total_size"`
	ChunkIndex   int    `json:"chunk_index"`
	ChunkCount   int    `json:"chunk_count"`
	Data         string `json:"data"`
}

type incomingPhoto struct {
	filename   string
	mimeType   string
	totalSize  int
	chunkCount int
	chunks     [][]byte
	received   int
}

func decodeContentPayload(body string) (*contentPayload, bool, error) {
	var payload contentPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, false, nil
	}
	if payload.Kind == "" {
		return nil, false, nil
	}
	return &payload, true, nil
}

func buildPhotoChunkPayloads(filename, mimeType string, bytes []byte) ([]string, string, error) {
	attachmentID := uuid.NewString()
	chunkCount := (len(bytes) + photoChunkSizeBytes - 1) / photoChunkSizeBytes
	if chunkCount == 0 {
		chunkCount = 1
	}
	payloads := make([]string, 0, chunkCount)
	for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
		start := chunkIndex * photoChunkSizeBytes
		end := start + photoChunkSizeBytes
		if end > len(bytes) {
			end = len(bytes)
		}
		payload, err := json.Marshal(contentPayload{
			Kind: contentKindPhotoChunk,
			PhotoChunk: &photoChunkPayload{
				AttachmentID: attachmentID,
				Filename:     sanitizeAttachmentName(filename),
				MIMEType:     mimeType,
				TotalSize:    len(bytes),
				ChunkIndex:   chunkIndex,
				ChunkCount:   chunkCount,
				Data:         base64.StdEncoding.EncodeToString(bytes[start:end]),
			},
		})
		if err != nil {
			return nil, "", fmt.Errorf("encode photo payload: %w", err)
		}
		payloads = append(payloads, string(payload))
	}
	return payloads, attachmentID, nil
}

func sanitizeAttachmentName(name string) string {
	clean := filepath.Base(strings.TrimSpace(name))
	clean = strings.ReplaceAll(clean, string(filepath.Separator), "_")
	if clean == "." || clean == "" {
		return "attachment.bin"
	}
	return clean
}
