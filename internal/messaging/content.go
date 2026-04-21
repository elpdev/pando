package messaging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/store"
	"github.com/google/uuid"
)

const (
	contentKindText            = "text"
	contentKindContactUpdate   = "contact-update"
	contentKindAttachmentChunk = "attachment-chunk"
	contentKindDeliveryAck     = "delivery-ack"
	contentKindContactRequest  = "contact-request"
	contentKindContactResponse = "contact-request-response"
	contentKindTyping          = "typing"
	TypingStateActive          = "active"
	TypingStateIdle            = "idle"
	AttachmentTypePhoto        = "photo"
	AttachmentTypeVoice        = "voice"
	AttachmentTypeFile         = "file"
	attachmentChunkSizeBytes   = 8 * 1024
	maxAttachmentSizeBytes     = 50 * 1024 * 1024
	maxAttachmentChunkCount    = maxAttachmentSizeBytes/attachmentChunkSizeBytes + 1
)

type deliveryAck struct {
	MessageID   string    `json:"message_id"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type contactRequest struct {
	Bundle identity.InviteBundle `json:"bundle"`
	Note   string                `json:"note,omitempty"`
}

type contactRequestResponse struct {
	Decision string                 `json:"decision"`
	Bundle   *identity.InviteBundle `json:"bundle,omitempty"`
}

type contentPayload struct {
	Kind               string                  `json:"kind"`
	Text               string                  `json:"text,omitempty"`
	ContactUpdate      *identity.InviteBundle  `json:"contact_update,omitempty"`
	AttachmentChunk    *attachmentChunkPayload `json:"attachment_chunk,omitempty"`
	DeliveryAck        *deliveryAck            `json:"delivery_ack,omitempty"`
	ContactRequest     *contactRequest         `json:"contact_request,omitempty"`
	ContactResponse    *contactRequestResponse `json:"contact_response,omitempty"`
	Typing             *typingIndicator        `json:"typing,omitempty"`
	RoomMessage        *roomMessage            `json:"room_message,omitempty"`
	RoomMembership     *roomMembership         `json:"room_membership,omitempty"`
	RoomHistoryRequest *roomHistoryRequest     `json:"room_history_request,omitempty"`
	RoomHistoryChunk   *roomHistoryChunk       `json:"room_history_chunk,omitempty"`
}

type typingIndicator struct {
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
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
	updatedAt      time.Time
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
	if len(bytes) > maxAttachmentSizeBytes {
		return nil, "", fmt.Errorf("%s exceeds attachment size limit of %d bytes", AttachmentLabel(attachmentType), maxAttachmentSizeBytes)
	}
	attachmentID := uuid.NewString()
	chunkCount := (len(bytes) + attachmentChunkSizeBytes - 1) / attachmentChunkSizeBytes
	if chunkCount == 0 {
		chunkCount = 1
	}
	if chunkCount > maxAttachmentChunkCount {
		return nil, "", fmt.Errorf("%s exceeds attachment chunk limit", AttachmentLabel(attachmentType))
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

func validateAttachmentMIMEType(path, mimeType, attachmentType string) error {
	switch attachmentType {
	case AttachmentTypePhoto:
		if strings.HasPrefix(mimeType, "image/") {
			return nil
		}
		return fmt.Errorf("%s is not a supported image file", path)
	case AttachmentTypeVoice:
		if strings.HasPrefix(mimeType, "audio/") {
			return nil
		}
		return fmt.Errorf("%s is not a supported audio file", path)
	case AttachmentTypeFile:
		return nil
	default:
		return fmt.Errorf("unsupported attachment type %q", attachmentType)
	}
}

func detectAttachmentMIMEType(filename string, bytes []byte, attachmentType string) string {
	mimeType := http.DetectContentType(bytes)
	ext := strings.ToLower(filepath.Ext(filename))
	if attachmentType == AttachmentTypeVoice && ext == ".m4a" && (mimeType == "application/octet-stream" || mimeType == "application/mp4" || mimeType == "video/mp4") {
		return "audio/mp4"
	}
	if mimeType == "application/octet-stream" && ext != "" {
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			return byExt
		}
	}
	return mimeType
}

func sanitizeAttachmentName(name string) string {
	clean := filepath.Base(strings.TrimSpace(name))
	clean = strings.ReplaceAll(clean, string(filepath.Separator), "_")
	if clean == "." || clean == "" {
		return "attachment.bin"
	}
	return clean
}

func AttachmentLabel(attachmentType string) string {
	switch attachmentType {
	case AttachmentTypePhoto:
		return "photo"
	case AttachmentTypeVoice:
		return "voice note"
	case AttachmentTypeFile:
		return "file"
	default:
		return "attachment"
	}
}

func NewAttachmentRecord(attachmentType, filename, mimeType, localPath string, size int64) *store.AttachmentRecord {
	return &store.AttachmentRecord{
		Type:      attachmentType,
		Filename:  sanitizeAttachmentName(filename),
		MIMEType:  mimeType,
		LocalPath: localPath,
		Size:      size,
	}
}

func AttachmentSentBody(attachmentType, filename string) string {
	return fmt.Sprintf("%s sent: %s", AttachmentLabel(attachmentType), sanitizeAttachmentName(filename))
}

func AttachmentReceivedBody(attachmentType, filename, path string) string {
	return fmt.Sprintf("%s received: %s saved to %s", AttachmentLabel(attachmentType), sanitizeAttachmentName(filename), path)
}
