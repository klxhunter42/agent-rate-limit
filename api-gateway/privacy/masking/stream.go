package masking

// StreamUnmasker handles chunk-by-chunk unmasking of SSE streaming responses.
// It maintains separate buffers for PII and secrets to handle partial placeholders
// that span chunk boundaries.
type StreamUnmasker struct {
	piiBuffer     string
	secretsBuffer string
	piiCtx        *MaskContext
	secretsCtx    *MaskContext
}

func NewStreamUnmasker(piiCtx, secretsCtx *MaskContext) *StreamUnmasker {
	return &StreamUnmasker{
		piiCtx:     piiCtx,
		secretsCtx: secretsCtx,
	}
}

// ProcessChunk unmask a single SSE text chunk.
// PII placeholders are unmasked first, then secrets on the result.
func (u *StreamUnmasker) ProcessChunk(chunk string) string {
	processed := chunk

	// Unmask PII first.
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		processed, u.piiBuffer = processStreamChunk(u.piiBuffer, processed, u.piiCtx)
	}

	// Then unmask secrets on the PII-unmasked result.
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		processed, u.secretsBuffer = processStreamChunk(u.secretsBuffer, processed, u.secretsCtx)
	}

	return processed
}

// Flush returns any remaining buffered content (call at stream end).
func (u *StreamUnmasker) Flush() string {
	result := ""
	if u.piiCtx != nil && u.piiBuffer != "" {
		result += u.piiCtx.RestorePlaceholders(u.piiBuffer)
		u.piiBuffer = ""
	}
	if u.secretsCtx != nil && u.secretsBuffer != "" {
		result += u.secretsCtx.RestorePlaceholders(u.secretsBuffer)
		u.secretsBuffer = ""
	}
	return result
}

// HasContexts returns true if there are any placeholders to unmask.
func (u *StreamUnmasker) HasContexts() bool {
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		return true
	}
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		return true
	}
	return false
}

func processStreamChunk(buffer, chunk string, ctx *MaskContext) (output, remaining string) {
	combined := buffer + chunk

	partialStart := FindPartialPlaceholderStart(combined)
	if partialStart < 0 {
		return ctx.RestorePlaceholders(combined), ""
	}

	safeToProcess := combined[:partialStart]
	toBuffer := combined[partialStart:]
	return ctx.RestorePlaceholders(safeToProcess), toBuffer
}
