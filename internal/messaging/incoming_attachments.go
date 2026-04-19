package messaging

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/store"
)

type incomingAttachmentAssembler struct {
	store   *store.ClientStore
	pending map[string]*incomingAttachment
}

func newIncomingAttachmentAssembler(store *store.ClientStore) *incomingAttachmentAssembler {
	return &incomingAttachmentAssembler{store: store, pending: make(map[string]*incomingAttachment)}
}

func (a *incomingAttachmentAssembler) handleChunk(peerAccountID string, chunk *attachmentChunkPayload) (string, bool, error) {
	if a.pending == nil {
		a.pending = make(map[string]*incomingAttachment)
	}
	now := time.Now().UTC()
	a.cleanup(now)
	if chunk == nil {
		return "", false, fmt.Errorf("attachment payload is required")
	}
	if chunk.AttachmentType != attachmentTypePhoto && chunk.AttachmentType != attachmentTypeVoice && chunk.AttachmentType != attachmentTypeFile {
		return "", false, fmt.Errorf("invalid attachment payload type")
	}
	if chunk.AttachmentID == "" || chunk.Filename == "" || chunk.TotalSize <= 0 || chunk.TotalSize > maxAttachmentSizeBytes || chunk.ChunkCount <= 0 || chunk.ChunkCount > maxAttachmentChunkCount || chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunkCount {
		return "", false, fmt.Errorf("invalid attachment payload metadata")
	}
	bytes, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return "", false, fmt.Errorf("decode attachment chunk: %w", err)
	}
	if len(bytes) == 0 || len(bytes) > attachmentChunkSizeBytes {
		return "", false, fmt.Errorf("invalid attachment chunk size")
	}
	key := peerAccountID + ":" + chunk.AttachmentID
	pending, ok := a.pending[key]
	if !ok {
		if len(a.pending) >= maxPendingIncomingAttachments {
			return "", false, fmt.Errorf("too many pending attachments")
		}
		if a.pendingCount(peerAccountID) >= maxPendingIncomingAttachmentsPeer {
			return "", false, fmt.Errorf("too many pending attachments for peer %s", peerAccountID)
		}
		pending = &incomingAttachment{
			attachmentType: chunk.AttachmentType,
			filename:       sanitizeAttachmentName(chunk.Filename),
			mimeType:       chunk.MIMEType,
			totalSize:      chunk.TotalSize,
			chunkCount:     chunk.ChunkCount,
			chunks:         make([][]byte, chunk.ChunkCount),
			updatedAt:      now,
		}
		a.pending[key] = pending
	}
	if pending.attachmentType != chunk.AttachmentType || pending.chunkCount != chunk.ChunkCount || pending.totalSize != chunk.TotalSize || pending.filename != sanitizeAttachmentName(chunk.Filename) {
		delete(a.pending, key)
		return "", false, fmt.Errorf("attachment payload does not match existing transfer")
	}
	pending.updatedAt = now
	if pending.chunks[chunk.ChunkIndex] == nil {
		pending.chunks[chunk.ChunkIndex] = bytes
		pending.received++
	}
	if pending.received != pending.chunkCount {
		return "", false, nil
	}
	assembled := make([]byte, 0, pending.totalSize)
	for _, part := range pending.chunks {
		if part == nil {
			return "", false, fmt.Errorf("attachment transfer is missing chunks")
		}
		assembled = append(assembled, part...)
	}
	if pending.totalSize > 0 && len(assembled) != pending.totalSize {
		return "", false, fmt.Errorf("attachment transfer size mismatch")
	}
	path, err := a.store.SaveAttachment(peerAccountID, chunk.AttachmentID, pending.filename, assembled)
	if err != nil {
		return "", false, err
	}
	delete(a.pending, key)
	return fmt.Sprintf("%s received: %s saved to %s", AttachmentLabel(pending.attachmentType), pending.filename, path), true, nil
}

func (a *incomingAttachmentAssembler) cleanup(now time.Time) {
	for key, pending := range a.pending {
		if pending == nil || now.Sub(pending.updatedAt) > incomingAttachmentTTL {
			delete(a.pending, key)
		}
	}
}

func (a *incomingAttachmentAssembler) pendingCount(peerAccountID string) int {
	count := 0
	prefix := peerAccountID + ":"
	for key := range a.pending {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
}
