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
	contentKindText            = "text"
	contentKindAttachmentChunk = "attachment-chunk"
	attachmentTypePhoto        = "photo"
	attachmentTypeVoice        = "voice"
	attachmentChunkSizeBytes   = 8 * 1024
)

type contentPayload struct {
	Kind            string                  `json:"kind"`
	Text            string                  `json:"text,omitempty"`
	AttachmentChunk *attachmentChunkPayload `json:"attachment_chunk,omitempty"`
}

type attachmentChunkPayload struct {
	AttachmentType string `json:"attachment_type"`
	AttachmentID   string `json:"attachment_id"`
	Filename       string `json:"filename"`
	MIMEType       string `json:"mime_type"`
	TotalSize      int    `json:"total_size"`
	ChunkIndex     int    `json:"chunk_index"`
	ChunkCount     int    `json:"chunk_count"`
	Data           string `json:"data"`
}

type incomingAttachment struct {
	attachmentType string
	filename       string
	mimeType       string
	totalSize      int
	chunkCount     int
	chunks         [][]byte
	received       int
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

func buildAttachmentChunkPayloads(attachmentType, filename, mimeType string, bytes []byte) ([]string, string, error) {
	attachmentID := uuid.NewString()
	chunkCount := (len(bytes) + attachmentChunkSizeBytes - 1) / attachmentChunkSizeBytes
	if chunkCount == 0 {
		chunkCount = 1
	}
	payloads := make([]string, 0, chunkCount)
	for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
		start := chunkIndex * attachmentChunkSizeBytes
		end := start + attachmentChunkSizeBytes
		if end > len(bytes) {
			end = len(bytes)
		}
		payload, err := json.Marshal(contentPayload{
			Kind: contentKindAttachmentChunk,
			AttachmentChunk: &attachmentChunkPayload{
				AttachmentType: attachmentType,
				AttachmentID:   attachmentID,
				Filename:       sanitizeAttachmentName(filename),
				MIMEType:       mimeType,
				TotalSize:      len(bytes),
				ChunkIndex:     chunkIndex,
				ChunkCount:     chunkCount,
				Data:           base64.StdEncoding.EncodeToString(bytes[start:end]),
			},
		})
		if err != nil {
			return nil, "", fmt.Errorf("encode attachment payload: %w", err)
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
