package masking

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

// ProcessChunk unmasks a single SSE text chunk with buffering for partial placeholders.
// Only use for text/thinking deltas within the same content block.
func (u *StreamUnmasker) ProcessChunk(chunk string) string {
	processed := chunk
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		processed, u.secretsBuffer = processStreamChunk(u.secretsBuffer, processed, u.secretsCtx)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		processed, u.piiBuffer = processStreamChunk(u.piiBuffer, processed, u.piiCtx)
	}
	return processed
}

// ReplaceDirect does unbuffered replacement on a standalone string.
// Use for partial_json and other independent SSE data to avoid
// cross-block buffer contamination.
func (u *StreamUnmasker) ReplaceDirect(text string) string {
	result := text
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		result = u.secretsCtx.RestorePlaceholders(result)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		result = u.piiCtx.RestorePlaceholders(result)
	}
	return result
}

// ReplaceDirectJSON does unbuffered JSON-safe replacement.
// Use for partial_json and raw SSE data lines where the replaced
// values must survive JSON string encoding.
func (u *StreamUnmasker) ReplaceDirectJSON(text string) string {
	result := text
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		result = u.secretsCtx.RestorePlaceholdersJSON(result)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		result = u.piiCtx.RestorePlaceholdersJSON(result)
	}
	return result
}

func (u *StreamUnmasker) Flush() string {
	// Process secrets first (innermost layer), then PII (outermost layer)
	// This matches the layering order in ProcessChunk/ReplaceDirect.
	result := ""
	if u.secretsCtx != nil && u.secretsBuffer != "" {
		result += u.secretsCtx.RestorePlaceholders(u.secretsBuffer)
		u.secretsBuffer = ""
	}
	combined := u.piiBuffer + result
	if u.piiCtx != nil && combined != "" {
		result = u.piiCtx.RestorePlaceholders(combined)
		u.piiBuffer = ""
	} else if u.piiBuffer != "" {
		result = u.piiBuffer + result
		u.piiBuffer = ""
	}
	return result
}

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
