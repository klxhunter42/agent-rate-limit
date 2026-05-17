#!/usr/bin/env python3
"""Apply H3+H4: Add lastRelayBlockIdx index-change flush in relay path."""

with open('proxy/anthropic.go', 'r') as f:
    content = f.read()

old = '''			if json.Unmarshal([]byte(data), &evt) == nil {
					var changed bool
					if evt.Delta.Text != "" {
						before := evt.Delta.Text
						if stripper != nil {'''

new = '''			if json.Unmarshal([]byte(data), &evt) == nil {
					// Flush on block index change to prevent cross-block buffer contamination
					if unmasker != nil && evt.Index != lastRelayBlockIdx && lastRelayBlockIdx >= 0 {
						if remaining := unmasker.Flush(); remaining != "" {
							remaining = masking.SanitizeGarbledOutput(remaining)
							if remaining != "" {
								escaped, _ := json.Marshal(remaining)
								fmt.Fprintf(w, "event: content_block_delta\\ndata: {\\"type\\":\\"content_block_delta\\",\\"index\\":%d,\\"delta\\":{\\"type\\":\\"text_delta\\",\\"text\\":%s}}\\n\\n", lastRelayBlockIdx, string(escaped))
							}
						}
					}
					if unmasker != nil {
						lastRelayBlockIdx = evt.Index
					}
					var changed bool
					if evt.Delta.Text != "" {
						before := evt.Delta.Text
						if stripper != nil {'''

idx1 = content.find(old)
if idx1 < 0:
    print("ERROR: Pattern not found")
    exit(1)
idx2 = content.find(old, idx1 + 1)
if idx2 < 0:
    print("ERROR: Second occurrence not found")
    exit(1)

result = content[:idx2] + new + content[idx2 + len(old):]

with open('proxy/anthropic.go', 'w') as f:
    f.write(result)
print("H3+H4 relay path index-change flush: DONE")
