# Token Optimizer Techniques: Before/After Reference

All 16 optimization stages in the API Gateway pipeline, with concrete examples showing what each one does to real input.

> **Full pipeline reference**: See [optimizer-pipeline.md](../optimizer-pipeline.md) for complete 17-stage reference with configuration and execution order.

---

## Table of Contents

1. [Pipeline Overview](#1-pipeline-overview)
2. [semantic_dedup](#2-semantic_dedup)
3. [message_text (Whitespace + Sentence Dedup)](#3-message_text)
4. [message_block_text](#4-message_block_text)
5. [message_block_tool_result](#5-message_block_tool_result)
6. [message_textcomp](#6-message_textcomp)
7. [chunker](#7-chunker)
8. [delta (Metrics Only)](#8-delta-metrics-only)
9. [sketch_dedup](#9-sketch_dedup)
10. [summarizer](#10-summarizer)
11. [caveman_input](#11-caveman_input)
12. [caveman_output (Style Injection)](#12-caveman_output-style-injection)
13. [textcomp (System Prompt)](#13-textcomp-system-prompt)
14. [toolcomp](#14-toolcomp)
15. [toolfilter](#15-toolfilter)
16. [waste (Post-Hoc Detection)](#16-waste-post-hoc-detection)

---

## 1. Pipeline Overview

```
  REQUEST FLOW THROUGH OPTIMIZER
  ===============================

  Client Request
       |
       v
  ┌─────────────────────────────────────────────────────────────┐
  │  SYSTEM PROMPT OPTIMIZATION (OptimizeSystemPrompt)          │
  │                                                             │
  │  [F7]  semantic_dedup  ── remove near-duplicate sentences   │
  │  [F1]  chunker         ── reorder for cache locality        │
  │  [F8]  delta           ── metrics only (no modification)    │
  │  [F9]  sketch_dedup    ── near-duplicate detection          │
  │  [F6]  summarizer      ── extractive summary (red budget)   │
  │  [F17] textcomp        ── regex filler removal              │
  │  [F16] caveman_input   ── regex input compression           │
  │  [F16] caveman_output  ── output style injection            │
  │  pordee          ── Thai output compression            │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
       |
       v
  ┌─────────────────────────────────────────────────────────────┐
  │  MESSAGE OPTIMIZATION (OptimizeMessages)                    │
  │                                                             │
  │  Per message content block:                                 │
  │    message_text           ── whitespace collapse + dedup    │
  │    message_textcomp       ── filler removal on strings      │
  │    message_block_text     ── whitespace collapse on blocks  │
  │    message_block_tool_result ── whitespace + toolcomp       │
  │    toolcomp               ── format-aware tool compression  │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
       |
       v
  ┌─────────────────────────────────────────────────────────────┐
  │  TOOL MANIFEST OPTIMIZATION                                 │
  │                                                             │
  │    desctrim    ── trim verbose tool descriptions            │
  │    toolfilter  ── keep only relevant tools (if >15)         │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
       |
       v
  ┌─────────────────────────────────────────────────────────────┐
  │  POST-PROXY FEEDBACK                                        │
  │                                                             │
  │    waste ── detect token waste patterns across requests     │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
```

---

## 2. semantic_dedup

**Stage:** F7 | **Where:** System prompt | **Method:** Jaccard similarity (threshold 0.7)

Removes near-duplicate sentences from system prompt. Keeps first occurrence, drops subsequent similar ones.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  "You are a helpful assistant. You should always respond in     │
  │   a helpful manner. When writing code, use modern practices.    │
  │   Make sure to use modern coding practices for all code.        │
  │   Be concise in your responses and keep answers brief."         │
  │                                                                 │
  │  ████████████████████████████████████████████  234 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  "You are a helpful assistant. When writing code, use modern    │
  │   practices. Be concise in your responses."                     │
  │                                                                 │
  │  ████████████████████████████████░░░░░░░░░░░░  112 chars        │
  │                                                                 │
  │  Saved: 122 chars (52%)                                         │
  │                                                                 │
  │  What was removed:                                              │
  │  - "You should always respond in a helpful manner"              │
  │    (~similar to "You are a helpful assistant")                  │
  │  - "Make sure to use modern coding practices for all code"      │
  │    (~similar to "use modern practices")                         │
  │  - "and keep answers brief"                                     │
  │    (~similar to "Be concise")                                   │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 3. message_text

**Stage:** Per message (string content) | **Method:** Whitespace collapse + sentence dedup

Applies to messages where `content` is a plain string (not content blocks).

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  "Can you help me fix this bug?   The error says                │
  │                                                                 │
  │                                                                 │
  │   'connection refused'.  Can you help me fix this bug?          │
  │   I think it's a port issue."                                   │
  │                                                                 │
  │  ████████████████████████████████████████████  142 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  "Can you help me fix this bug? The error says 'connection      │
  │   refused'. I think it's a port issue."                         │
  │                                                                 │
  │  ████████████████████████████████░░░░░░░░░░░░  102 chars        │
  │                                                                 │
  │  Saved: 40 chars (28%)                                          │
  │                                                                 │
  │  What was removed:                                              │
  │  - Triple spaces collapsed to single                            │
  │  - Triple newlines collapsed to double                          │
  │  - "Can you help me fix this bug?" (exact duplicate sentence)   │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 4. message_block_text

**Stage:** Per message (content block type="text") | **Method:** Whitespace collapse + sentence dedup

Same as message_text but for structured content blocks (`[{"type":"text","text":"..."}]`).

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  content: [                                                     │
  │    {"type": "text", "text": "Look at this file:\n\n\n\n         │
  │     The file contains the config.  The file contains            │
  │     the config for the database."}                              │
  │  ]                                                              │
  │                                                                 │
  │  ████████████████████████████████████████████  120 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  content: [                                                     │
  │    {"type": "text", "text": "Look at this file:\n\nThe file     │
  │     contains the config for the database."}                     │
  │  ]                                                              │
  │                                                                 │
  │  ████████████████████████████░░░░░░░░░░░░░░░░   82 chars        │
  │                                                                 │
  │  Saved: 38 chars (32%)                                          │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 5. message_block_tool_result

**Stage:** Per message (content block type="tool_result") | **Method:** Whitespace collapse on `content` field

Same whitespace optimization, but operates on the `content` field instead of `text` field of tool_result blocks.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  content: [                                                     │
  │    {"type": "tool_result", "content": "NAME           STATUS    │
  │                                                                 │
  │     pod-api-7f8d    Running                                     │
  │     pod-web-9a2c    Running                                     │
  │                                                                 │
  │                                                                 │
  │     pod-db-3b1e     CrashLoopBackOff"}                          │
  │  ]                                                              │
  │                                                                 │
  │  ████████████████████████████████████████████  148 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  content: [                                                     │
  │    {"type": "tool_result", "content": "NAME           STATUS    │
  │     pod-api-7f8d    Running                                     │
  │     pod-web-9a2c    Running                                     │
  │                                                                 │
  │     pod-db-3b1e     CrashLoopBackOff"}                          │
  │  ]                                                              │
  │                                                                 │
  │  ████████████████████████████████████░░░░░░░░  136 chars        │
  │                                                                 │
  │  Saved: 12 chars (8%)                                           │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 6. message_textcomp

**Stage:** Per message (string content) | **Method:** Regex filler removal after whitespace dedup

Runs after message_text. Removes English filler phrases and hedge words from string message content.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  "I was wondering if you could maybe help me figure out why     │
  │   the deployment is failing. I would like to basically          │
  │   understand the root cause. Could you please also sort of      │
  │   check the logs?"                                              │
  │                                                                 │
  │  ████████████████████████████████████████████  185 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  "help me figure out why the deployment is failing.             │
  │   understand the root cause. check the logs?"                   │
  │                                                                 │
  │  ██████████████████████████░░░░░░░░░░░░░░░░░░  104 chars        │
  │                                                                 │
  │  Saved: 81 chars (44%)                                          │
  │                                                                 │
  │  What was removed:                                              │
  │  - "I was wondering if you could" -> filler phrase              │
  │  - "maybe" -> hedge word                                        │
  │  - "I would like to" -> filler phrase                           │
  │  - "basically" -> hedge word                                    │
  │  - "Could you please" -> filler phrase                          │
  │  - "sort of" -> hedge word                                      │
  │  - "also" -> hedge word                                         │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 7. chunker

**Stage:** F1 | **Where:** System prompt | **Method:** Rabin-Karp rolling hash + Redis stability tracking

Does NOT remove content. Reorders chunks so stable (previously seen) chunks come first, improving prompt cache hit rates.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  System prompt (order as written):                              │
  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐             │
  │  │ NEW content  │ │ STABLE chunk │ │ NEW content  │             │
  │  │ (novel)      │ │ (seen 5x)    │ │ (novel)      │             │
  │  │ 400 chars    │ │ 600 chars    │ │ 300 chars    │             │
  │  └──────────────┘ └──────────────┘ └──────────────┘             │
  │                                                                 │
  │  Prompt cache: MISS (new content at start breaks cache prefix)  │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  System prompt (reordered):                                     │
  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐             │
  │  │ STABLE chunk │ │ NEW content  │ │ NEW content  │             │
  │  │ (seen 5x)    │ │ (novel)      │ │ (novel)      │             │
  │  │ 600 chars    │ │ 400 chars    │ │ 300 chars    │             │
  │  └──────────────┘ └──────────────┘ └──────────────┘             │
  │                                                                 │
  │  Prompt cache: HIT on first 600 chars (cache prefix match)      │
  │  Cost savings: 600 cached tokens at $3/1M = $0.0018/request     │
  │                                                                 │
  │  Key insight: No text removed. Benefit is cache hit rate.       │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 8. delta (Metrics Only)

**Stage:** F8 | **Where:** System prompt | **Method:** LCS diff against Redis baseline

Does NOT modify content. Computes how much could be saved if delta encoding were used, purely for metrics.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  Input: "Configure the API gateway with TLS enabled..."         │
  │  (same system prompt as last 5 requests, minor changes)         │
  │                                                                 │
  │  Delta computation:                                             │
  │  ┌──────────────────────────────────────────────────┐           │
  │  │ Baseline (Redis): 2450 chars                     │           │
  │  │ Current input:   2480 chars                     │            │
  │  │ LCS overlap:     2100 chars (85% similar)       │            │
  │  │ Potential saved:  2100 chars                     │           │
  │  └──────────────────────────────────────────────────┘           │
  │                                                                 │
  │  Output: ORIGINAL CONTENT UNCHANGED                             │
  │  Metric recorded: delta_metrics = 2100 chars potential savings  │
  │                                                                 │
  │  Why not apply: Sending diffs to LLMs corrupts meaning.         │
  │  Value: Identifies sessions where caching would help.           │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 9. sketch_dedup

**Stage:** F9 | **Where:** System prompt | **Method:** FNV-1a bit sketch (simhash) + Hamming similarity

Detects near-duplicate system prompts across requests in the same session. If a prompt is >=85% similar to a previous one, flags it as duplicate.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  Request 1 system prompt:                                       │
  │  "You are a coding assistant. Follow these rules: ..."          │
  │  Sketch: [10110101 01101011 ...] (128 bits)                     │
  │  Stored in Redis                                                │
  │                                                                 │
  │  Request 2 system prompt (same session):                        │
  │  "You are a coding assistant. Follow these rules: ..."          │
  │  Sketch: [10110101 01101011 ...] (128 bits)                     │
  │  Similarity to Request 1: 0.97 (97% similar)                    │
  │  Threshold: 0.85                                                │
  │                                                                 │
  │  Result: isDuplicate=true, charsSaved=len(content)              │
  │  Action: Caller can skip sending or use cached response         │
  │                                                                 │
  │  ┌──────────────────────────────────────────────────┐           │
  │  │ Request 1:  ████████████████████████████  2000   │           │
  │  │ Request 2:  ████████████████████████████  2000   │           │
  │  │             (97% similar = near-duplicate)       │           │
  │  │ Saved:      2000 chars (if caller skips)         │           │
  │  └──────────────────────────────────────────────────┘           │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 10. summarizer

**Stage:** F6 | **Where:** System prompt | **Method:** TextRank (PageRank on sentences) | **Only:** Red budget (>80% used)

Extractive summarization that keeps the most important sentences. Only activates when token budget is critical.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (red budget, system prompt too long)                    │
  │                                                                 │
  │  "You are a code review assistant. When reviewing code, you     │
  │   must check for security vulnerabilities, including SQL        │
  │   injection, XSS, and CSRF. You should also verify that         │
  │   error handling is proper and that logging doesn't leak        │
  │   sensitive data. Check for performance issues like N+1         │
  │   queries and unbounded loops. Ensure test coverage is          │
  │   adequate for new code. Verify that API contracts are          │
  │   documented. Make sure dependencies are up to date and         │
  │   don't have known vulnerabilities. Check that the code         │
  │   follows the project's style guide. Verify that secrets        │
  │   are not hardcoded. Ensure proper input validation             │
  │   exists at system boundaries."                                 │
  │                                                                 │
  │  ████████████████████████████████████████████  620 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (TextRank top sentences, ~30% ratio)                     │
  │                                                                 │
  │  "You are a code review assistant. When reviewing code, you     │
  │   must check for security vulnerabilities, including SQL        │
  │   injection, XSS, and CSRF. Ensure proper input validation      │
  │   exists at system boundaries. Verify that secrets are not      │
  │   hardcoded."                                                   │
  │                                                                 │
  │  ████████████████████████████░░░░░░░░░░░░░░░░  234 chars        │
  │                                                                 │
  │  Saved: 386 chars (62%)                                         │
  │  Method: PageRank scored "security" + "boundaries" +            │
  │          "secrets" sentences highest (most connected in graph)  │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 11. caveman_input

**Stage:** F16 | **Where:** System prompt | **Method:** Regex compression (mask-apply-unmask)

Compresses input text by removing pleasantries, instruction fluff, and verbose synonyms. Protects code blocks, URLs, and file paths.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  "I'd be happy to help you with this! Make sure to              │
  │   implement a solution for the authentication bug. You          │
  │   should always remember to utilize proper error handling       │
  │   when making API calls. The timeout is approximately           │
  │   30 seconds. In the event that the connection fails,           │
  │   you need to initiate a reconnection attempt."                 │
  │                                                                 │
  │  ████████████████████████████████████████████  310 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  "Fix the authentication bug. Use proper error handling         │
  │   for API calls. Timeout ~30s. If connection fails,             │
  │   reconnect."                                                   │
  │                                                                 │
  │  ██████████████████████░░░░░░░░░░░░░░░░░░░░░  118 chars         │
  │                                                                 │
  │  Saved: 192 chars (62%)                                         │
  │                                                                 │
  │  Transformations:                                               │
  │  "I'd be happy to help you with this!"  -> removed (pleasantry) │
  │  "Make sure to"                         -> removed (fluff)      │
  │  "implement a solution for"             -> "fix" (synonym)      │
  │  "You should always remember to"        -> removed (fluff)      │
  │  "utilize"                              -> "use" (synonym)      │
  │  "approximately"                        -> "~" (synonym)        │
  │  "In the event that"                    -> "if" (synonym)       │
  │  "initiate a reconnection attempt"      -> "reconnect"          │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 12. caveman_output (Style Injection)

**Stage:** F16 | **Where:** System prompt (append) | **Method:** Instruction injection

Appends terse output-style instructions to the system prompt. The model reads these and produces shorter output.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (system prompt as-is)                                   │
  │                                                                 │
  │  System: "You are Claude Code, Anthropic's official CLI..."     │
  │                                                                 │
  │  Model output (~500 tokens):                                    │
  │  "Great question! I'd be happy to explain how Kubernetes        │
  │   services work. Basically, a Kubernetes Service is an          │
  │   abstraction that defines a logical set of pods and a          │
  │   policy to access them. There are several types of             │
  │   services you should know about. First, ClusterIP which        │
  │   is the default type..."                                       │
  │  ████████████████████████████████████████████  500 tokens       │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (caveman tier=full appended to system prompt)            │
  │                                                                 │
  │  System: "You are Claude Code...                                │
  │           [OUTPUT STYLE - full]                                 │
  │           Be extremely terse. Code only when asked.             │
  │           No explanations unless requested.                     │
  │           If the answer fits in one line, use one line."        │
  │                                                                 │
  │  Model output (~250 tokens):                                    │
  │  "K8s Service = abstraction over pods.                          │
  │   ClusterIP (default): internal only.                           │
  │   NodePort: expose on node IP:port.                             │
  │   LoadBalancer: cloud provider LB."                             │
  │  ██████████████████████████░░░░░░░░░░░░░░░░░░  250 tokens       │
  │                                                                 │
  │  Output tokens saved: ~250 (50%)                                │
  │  Input chars added: ~200 (style injection text)                 │
  │  Net savings: positive for any response > 50 tokens             │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

### Caveman Tier Comparison

```
  ┌────────────────────────────────────────────────────────────────┐
  │                                                                │
  │  User: "explain Docker volumes"                                │
  │                                                                │
  │  Normal (~400 tok):                                            │
  │  "Certainly! Docker volumes are a mechanism for persisting     │
  │   data generated by and used by Docker containers..."          │
  │  ██████████████████████████████████████████████████████████    │
  │                                                                │
  │  Lite (~280 tok): "concise, bullet points, skip filler"        │
  │  "Docker volumes persist data beyond container lifecycle.      │
  │   Types: named volumes, bind mounts, tmpfs..."                 │
  │  ████████████████████████████████████████░░░░░░░░░░░░░░░░░     │
  │                                                                │
  │  Full (~200 tok): "extremely terse, no filler, tables OK"      │
  │  "Docker volumes: persistent storage for containers.           │
  │   | Type | Use case |                                          │
  │   | named | prod data |..."                                    │
  │  ████████████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░      │
  │                                                                │
  │  Ultra (~100 tok): "raw output, symbols, zero prose"           │
  │  "named: docker volume create; bind: -v /host:/container;      │
  │   tmpfs: --tmpfs /run"                                         │
  │  ██████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░       │
  │                                                                │
  └────────────────────────────────────────────────────────────────┘
```

---

## 13. textcomp (System Prompt)

**Stage:** F17 | **Where:** System prompt | **Method:** Regex filler removal (same engine as message_textcomp)

Runs on system prompt text (not messages). Removes English filler, hedges, and verbose expressions.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE                                                         │
  │                                                                 │
  │  System: "It is important to note that you should always        │
  │   make sure to validate all user inputs. As a matter of         │
  │   fact, input validation is basically the first line of         │
  │   defense. In order to ensure security, you need to             │
  │   sanitize all data prior to processing."                       │
  │                                                                 │
  │  ████████████████████████████████████████████  270 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER                                                          │
  │                                                                 │
  │  System: "Validate all user inputs. Input validation is the     │
  │   first line of defense. Sanitize all data before processing."  │
  │                                                                 │
  │  ██████████████████████████░░░░░░░░░░░░░░░░░░  124 chars        │
  │                                                                 │
  │  Saved: 146 chars (54%)                                         │
  │                                                                 │
  │  Removed:                                                       │
  │  - "It is important to note that"                               │
  │  - "you should always make sure to"                             │
  │  - "As a matter of fact"                                        │
  │  - "basically"                                                  │
  │  - "In order to ensure" -> "before"                             │
  │  - "prior to" -> "before"                                       │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 14. toolcomp

**Stage:** Message tool_result blocks | **Method:** Format-aware compression

Auto-detects content format (JSON, shell ls, table, diff, log, prose) and applies format-specific truncation.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (shell ls output, 200 files)                            │
  │                                                                 │
  │  tool_result content:                                           │
  │  "file001.txt  file002.txt  file003.txt  file004.txt            │
  │   file005.txt  file006.txt  file007.txt  file008.txt            │
  │   file009.txt  file010.txt  ...  (190 more files) ...           │
  │   file198.txt  file199.txt  file200.txt"                        │
  │                                                                 │
  │  ████████████████████████████████████████████  4200 chars       │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (shell ls format, MaxLines=50)                           │
  │                                                                 │
  │  tool_result content:                                           │
  │  "file001.txt  file002.txt  file003.txt  file004.txt            │
  │   file005.txt  file006.txt  file007.txt  file008.txt            │
  │   file009.txt  file010.txt  file011.txt  file012.txt            │
  │   ... 190 more files ...                                        │
  │   file199.txt  file200.txt"                                     │
  │                                                                 │
  │  ████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░  580 chars         │
  │                                                                 │
  │  Saved: 3620 chars (86%)                                        │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘


  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (JSON tool response, unformatted)                       │
  │                                                                 │
  │  tool_result content:                                           │
  │  {                                                              │
  │    "items": [                                                   │
  │      { "id": 1, "name": "alpha",   "status": "running" },       │
  │      { "id": 2, "name": "beta",    "status": "stopped" },       │
  │      { "id": 3, "name": "gamma",   "status": "running" }        │
  │    ],                                                           │
  │    "total": 3                                                   │
  │  }                                                              │
  │                                                                 │
  │  ████████████████████████████████████████████  230 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (json.Compact)                                           │
  │                                                                 │
  │  {"items":[{"id":1,"name":"alpha","status":"running"},          │
  │   {"id":2,"name":"beta","status":"stopped"},                    │
  │   {"id":3,"name":"gamma","status":"running"}],"total":3}        │
  │                                                                 │
  │  ██████████████████████████████░░░░░░░░░░░░░░  162 chars        │
  │                                                                 │
  │  Saved: 68 chars (30%)                                          │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘


  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (diff output, lots of context)                          │
  │                                                                 │
  │  tool_result content:                                           │
  │  "diff --git a/main.go b/main.go                                │
  │   index abc1234..def5678 100644                                 │
  │   --- a/main.go                                                 │
  │   +++ b/main.go                                                 │
  │   @@ -10,15 +10,15 @@                                           │
  │   context line 1                          (unchanged)           │
  │   context line 2                          (unchanged)           │
  │   context line 3                          (unchanged)           │
  │   -old line A                                                   │
  │   -old line B                                                   │
  │   +new line A                                                   │
  │   +new line B                                                   │
  │   context line 7                          (unchanged)           │
  │   context line 8                          (unchanged)           │
  │   context line 9                          (unchanged)           │
  │   -old line C                                                   │
  │   +new line C"                                                  │
  │                                                                 │
  │  ████████████████████████████████████████████  580 chars        │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (diff format, context trimmed)                           │
  │                                                                 │
  │  "diff --git a/main.go b/main.go                                │
  │   --- a/main.go                                                 │
  │   +++ b/main.go                                                 │
  │   @@ -10,15 +10,15 @@                                           │
  │   -old line A                                                   │
  │   -old line B                                                   │
  │   +new line A                                                   │
  │   +new line B                                                   │
  │   context line 7                                                │
  │   -old line C                                                   │
  │   +new line C"                                                  │
  │                                                                 │
  │  ██████████████████████░░░░░░░░░░░░░░░░░░░░░  310 chars         │
  │                                                                 │
  │  Saved: 270 chars (47%)                                         │
  │  Kept: headers, all +/- changed lines, 1 context line           │
  │  Dropped: unchanged context lines                               │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 15. toolfilter

**Stage:** Request tools manifest | **Method:** Intent scoring + keyword overlap

When request has >15 tools, scores each tool by relevance to user's intent and keeps only the top 15. Always keeps critical tools (Read, Edit, Write, Bash).

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  BEFORE (30 tools in manifest)                                  │
  │                                                                 │
  │  tools: [                                                       │
  │    Read, Edit, Write, Bash,         (core - always kept)        │
  │    WebSearch, WebFetch,             (search tools)              │
  │    Grep, Glob,                      (search tools)              │
  │    NotebookEdit,                    (data tools)                │
  │    TaskOutput, TaskStop,            (task tools)                │
  │    mcp__github__create_issue,       (GitHub tools)              │
  │    mcp__github__list_prs,           (GitHub tools)              │
  │    mcp__github__review_pr,          (GitHub tools)              │
  │    mcp__jira__create_ticket,        (Jira tools)                │
  │    mcp__jira__search,               (Jira tools)                │
  │    mcp__slack__send_message,        (Slack tools)               │
  │    mcp__grafana__query,             (Grafana tools)             │
  │    mcp__k8s__apply,                 (K8s tools)                 │
  │    mcp__k8s__get_pods,              (K8s tools)                 │
  │    mcp__terraform__plan,            (Terraform tools)           │
  │    ... 15 more                                                  │
  │  ]                                                              │
  │  Total: 30 tools                                                │
  │  Token cost: ~4500 tokens for tool descriptions                 │
  │                                                                 │
  ├─────────────────────────────────────────────────────────────────┤
  │  AFTER (user asks "fix the login bug")                          │
  │                                                                 │
  │  Intent classified: code                                        │
  │  Keywords: "fix", "login", "bug"                                │
  │                                                                 │
  │  tools: [                                                       │
  │    Read, Edit, Write, Bash,         (core - always kept)        │
  │    Grep, Glob,                      (code search - high score)  │
  │    WebSearch,                       (search - medium score)     │
  │    NotebookEdit,                    (data - low score, cut)     │
  │    ... top 15 by score                                          │
  │  ]                                                              │
  │                                                                 │
  │  Dropped: mcp__slack__*, mcp__grafana__*, mcp__jira__*,         │
  │           mcp__k8s__*, mcp__terraform__*                        │
  │  (irrelevant to "fix login bug" intent)                         │
  │                                                                 │
  │  Total: 15 tools                                                │
  │  Token cost: ~2250 tokens (50% reduction)                       │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 16. waste (Post-Hoc Detection)

**Stage:** Post-proxy feedback | **Method:** Pattern analysis across session requests

Not a compressor. Analyzes request patterns after the fact to identify waste. 7 detectors.

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  Session: user-session-abc123                                   │
  │                                                                 │
  │  Request history (last 30 min):                                 │
  │  ┌────────────────────────────────────────────────────────┐     │
  │  │ Req  Input    Output   Notes                          │      │
  │  │ #1   50000    0        empty response (timeout)        │     │
  │  │ #2   50000    0        retry, same input               │     │
  │  │ #3   50000    0        retry, same input               │     │
  │  │ #4   50000    0        retry, same input               │     │
  │  │ #5   2000     150      finally works (different input)  │    │
  │  │ #6   120000   50       massive input, tiny output      │     │
  │  │ #7   120000   45       same pattern                    │     │
  │  │ #8   120000   50       same pattern                    │     │
  │  └────────────────────────────────────────────────────────┘     │
  │                                                                 │
  │  Waste findings:                                                │
  │  ┌────────────────────────────────────────────────────────┐     │
  │  │                                                        │     │
  │  │  [HIGH] empty_response                                 │     │
  │  │  4/8 requests returned 0 output tokens                 │     │
  │  │  Wasted: ~200,000 input tokens ($0.60)                 │     │
  │  │  Suggest: check upstream health, reduce input size     │     │
  │  │                                                        │     │
  │  │  [HIGH] retry_churn                                    │     │
  │  │  3 identical retries with 0 output                     │     │
  │  │  Wasted: ~150,000 tokens                               │     │
  │  │  Suggest: implement circuit breaker                    │     │
  │  │                                                        │     │
  │  │  [MEDIUM] low_value_response                           │     │
  │  │  3 requests: 120K input -> ~50 output tokens           │     │
  │  │  Ratio: 0.04% (should be > 0.1%)                       │     │
  │  │  Suggest: reduce context window, use summarizer        │     │
  │  │                                                        │     │
  │  └────────────────────────────────────────────────────────┘     │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## Cumulative Savings: All Stages Combined

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  Typical request with 10K char system prompt + 5 messages:      │
  │                                                                 │
  │  Stage                  Before    After    Saved    %           │
  │  ─────────────────────────────────────────────────────          │
  │  semantic_dedup          10000     8200     1800    18%         │
  │  textcomp (sys)           8200     6800     1400    17%         │
  │  caveman_input            6800     5400     1400    21%         │
  │  caveman_output (inject)  5400     5600      -200   -4%         │
  │  message_text (x5)        4000     3200      800    20%         │
  │  message_textcomp         3200     2400      800    25%         │
  │  toolcomp (tool_results)  8000     3000     5000    63%         │
  │  toolfilter (30->15)      4500     2250     2250    50%         │
  │  ─────────────────────────────────────────────────────          │
  │  TOTAL                   50100    38850    13250    26%         │
  │                                                                 │
  │  But output tokens also reduced by caveman_output:              │
  │  Output without style:    500 tokens                            │
  │  Output with style:       250 tokens                            │
  │  Output saved:            250 tokens (50%)                      │
  │                                                                 │
  │  True total savings (input + output):                           │
  │  Input chars saved:  13,250 (~3,312 tokens)                     │
  │  Output tokens saved:       250                                 │
  │  Cost saved per request: ~$0.005 (Sonnet)                       │
  │  Cost saved per 10K req:  ~$50/day                              │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## Technique Quick Reference

| Stage | Target | Method | Avg Savings | When |
|---|---|---|---|---|
| semantic_dedup | System prompt | Jaccard similarity | 15-25% | Always |
| chunker | System prompt | Rabin-Karp reorder | 0% chars, cache hit + | Always |
| delta | System prompt | LCS diff | 0% (metrics only) | Always |
| sketch_dedup | System prompt | Simhash | Flags duplicates | Always |
| summarizer | System prompt | TextRank | 60-70% | Red budget only |
| textcomp | System prompt, messages | Regex filler removal | 30-50% | Always |
| caveman_input | System prompt | Regex compression | 40-60% | Content > 500 chars |
| caveman_output | System prompt (append) | Style injection | 50% output tokens | Content > 500 chars |
| message_text | Message strings | Whitespace + dedup | 20-30% | Always |
| message_block_text | Text blocks | Whitespace + dedup | 20-30% | Always |
| message_block_tool_result | Tool result blocks | Whitespace + dedup | 10-20% | Always |
| toolcomp | Tool result content | Format-aware truncation | 30-86% | Content > 256 chars |
| toolfilter | Tool manifest | Intent scoring | 30-50% tools | > 15 tools |
| waste | Post-hoc analysis | Pattern detection | Reports only | Background scan |
| **pordee** | **System prompt** | **Thai injection** | **60-75% output** | **Thai text detected** |
