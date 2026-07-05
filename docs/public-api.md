# Public API

Consortium exposes an OpenAI-compatible text-generation API for applications
that already use OpenAI SDKs or `/v1` HTTP clients. Point your client at the
Consortium base URL, use a Consortium API key, and requests execute through the
model route or workflow selected by your operator.

This is not full OpenAI platform parity. The public API focuses on Chat
Completions, Responses, model discovery, usage accounting, and stored
generation resources for text workloads.

## Quick Start

Local backend base URL:

```text
http://localhost:8080/v1
```

Production base URL:

```text
https://your-consortium-host.example/v1
```

All runtime requests require:

```text
Authorization: Bearer <Consortium API key>
```

Create a key from the admin UI or with `conctl`:

```bash
./bin/conctl api key-create \
  --name app-server \
  --requests-per-minute 60 \
  --tokens-per-minute 120000 \
  --yes
```

The full secret is returned once. Store it as you would an OpenAI API key.

### curl

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $CONSORTIUM_OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "Write a one-sentence status update."}
    ]
  }'
```

### Python SDK

```python
import os

from openai import OpenAI

client = OpenAI(
    api_key=os.environ["CONSORTIUM_OPENAI_API_KEY"],
    base_url="http://localhost:8080/v1",
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Summarize this in one sentence."}],
)

print(response.choices[0].message.content)
```

### Node SDK

```js
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.CONSORTIUM_OPENAI_API_KEY,
  baseURL: "http://localhost:8080/v1",
});

const response = await client.responses.create({
  model: "gpt-4o-mini",
  input: "Give me a concise release note.",
});

console.log(response.output_text);
```

## Models And Routing

Model names are configured by Consortium operators as API model routes. A route
can point to a direct OpenRouter-backed model or to a Consortium workflow.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/models` | List enabled API model routes. |
| `GET` | `/v1/models/{model}` | Retrieve one enabled model route. |

Model objects are OpenAI-shaped and include `id`, `object: "model"`, `created`,
and `owned_by: "consortium"`.

For generation requests, an unknown model can use the enabled default route.
Model retrieval for an unknown or disabled model returns `404`.

## Chat Completions

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/chat/completions` | Create a Chat Completion. |
| `GET` | `/v1/chat/completions` | List stored Chat Completions for the API key. |
| `GET` | `/v1/chat/completions/{id}` | Retrieve one stored Chat Completion. |
| `GET` | `/v1/chat/completions/{id}/messages` | List stored messages. |

Supported request basics:

- `model` and `messages` are required.
- Supported roles: `system`, `developer`, `user`, `assistant`, `tool`, `function`.
- Content is text-only.
- `temperature`, `top_p`, `max_completion_tokens`, `max_tokens`, `stop`, `seed`,
  `metadata`, `response_format`, `tools`, `tool_choice`,
  `parallel_tool_calls`, `reasoning_effort`, `stream`, `stream_options`, and
  `store` are accepted where the selected route can honor them.
- `session_id`, `prompt_cache_key`, `prompt_cache_retention`, `n: 1`,
  `logprobs: false`, `modalities: ["text"]`, `user`, `service_tier`,
  `verbosity`, and `safety_identifier` are accepted compatibility fields.
- `max_completion_tokens` takes precedence over `max_tokens`.
- `prompt_cache_retention` must be `in_memory`, `in-memory`, or `24h`.
- Metadata is limited to 16 string pairs, with keys up to 64 characters and
  values up to 512 characters.

Chat Completions are stored only when `store: true`. Stored resources are
scoped to the API key that created them.

```json
{
  "model": "gpt-4o-mini",
  "store": true,
  "messages": [
    {"role": "system", "content": "Be concise."},
    {"role": "user", "content": "What changed in this release?"}
  ]
}
```

## Responses

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/responses` | Create a Response. |
| `GET` | `/v1/responses/{id}` | Retrieve one stored Response. |
| `GET` | `/v1/responses/{id}/input_items` | List stored input items. |
| `POST` | `/v1/responses/{id}/cancel` | Cancel an in-progress background Response. |

There is no `GET /v1/responses` list endpoint yet.

Supported request basics:

- `model` and `input` are required.
- `input` can be a string, text/message object, or an array of supported text,
  message, `function_call`, and `function_call_output` items.
- `instructions`, `previous_response_id`, `temperature`, `top_p`,
  `max_output_tokens`, `stop`, `seed`, `metadata`, `text.format`,
  `response_format`, `tools`, `tool_choice`, `parallel_tool_calls`,
  `reasoning.effort`, `stream`, `background`, `store`, `prompt_cache_key`, and
  `prompt_cache_retention` are accepted where the selected route can honor them.
- `response_format` takes precedence over `text.format`.
- `prompt_cache_retention` must be `in_memory`, `in-memory`, or `24h`.
- Non-streaming Responses default to `store: true`.

```json
{
  "model": "gpt-4o-mini",
  "input": "Draft a short user-facing incident update.",
  "text": {
    "format": {
      "type": "json_schema",
      "json_schema": {
        "name": "incident_update",
        "schema": {
          "type": "object",
          "properties": {
            "summary": {"type": "string"},
            "next_steps": {"type": "string"}
          },
          "required": ["summary", "next_steps"],
          "additionalProperties": false
        }
      }
    }
  }
}
```

### Background Responses

Use `background: true` for long-running workflow-backed requests. Consortium
returns an immediate stored `in_progress` Response after the workflow job is
accepted. Poll `GET /v1/responses/{id}` until the status becomes `completed`,
`failed`, or `cancelled`.

Background Responses require storage. `background: true` with `store: false` is
rejected. `background: true` with `stream: true` is also rejected until resumable
stream cursors exist.

```bash
curl http://localhost:8080/v1/responses \
  -H "Authorization: Bearer $CONSORTIUM_OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","input":"Run the long analysis.","background":true}'
```

## Streaming

Set `stream: true` to receive server-sent events.

Consortium streaming is lifecycle/final-content streaming. The connection opens
immediately and heartbeat comments keep it alive while the routed workflow runs.
Final text and function-call events are emitted after workflow completion.
Responses final events include usage; Chat streams include usage only when
`stream_options.include_usage` is true. This is not provider token streaming,
live function-argument streaming, or durable stream resume.

Chat streams use Chat-Completions-style chunks and end with:

```text
data: [DONE]
```

Responses streams use typed event frames such as `response.created`,
`response.in_progress`, output/content events, and `response.completed`.
Failures emit `response.failed`. Responses streams end at the terminal
Responses event rather than a `[DONE]` marker.

Streaming Responses are not stored as retrievable `/v1/responses/{id}`
resources. Use non-streaming or background Responses when you need retrieval or
`input_items`.

## Function Tools

Function tool definitions are supported for direct-model routes and passed to
the upstream provider. Hosted tools are not supported.

Direct-model Chat responses can expose provider tool calls as public
`message.tool_calls`. Workflow routes suppress internal provider tool calls so
workflow implementation details do not leak into public Chat responses.

Responses can store and replay typed `function_call` and `function_call_output`
items. A `function_call_output.call_id` must match a function call from the
current request or the same-key `previous_response_id` chain. The typed state is
rendered into deterministic workflow text at the provider boundary; it is not
native OpenAI conversation-state execution.

## Idempotency

Send `Idempotency-Key` on create requests that may be retried:

```bash
curl http://localhost:8080/v1/responses \
  -H "Authorization: Bearer $CONSORTIUM_OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: job-123" \
  -d '{"model":"gpt-4o-mini","input":"Generate the report."}'
```

Behavior:

- Keys are scoped by API key and endpoint.
- Maximum key length is 255 bytes.
- Replay records expire after 24 hours.
- Reusing a key with a different request body returns `409`.
- Non-streaming duplicate requests wait up to 30 seconds for the original
  response and then replay the stored JSON when available.
- Streaming requests do not byte-replay stored SSE streams.
- Successful `store:false` requests do not retain generated response bodies for
  idempotency replay.

## Storage And Privacy

`store:false` suppresses public OpenAI-compatible stored resources and
successful idempotency replay bodies. It is not a deletion or no-retention
guarantee: workflow jobs, usage rows, provider accounting, logs, and operational
audit data can still persist until an operator retention process removes them.

Resource reads are API-key scoped. A valid object ID owned by another API key is
returned as `404`.

## Usage, Limits, And Errors

Generation requests and authenticated generation failures record usage with
request status, HTTP status, requested/resolved model, workflow route, token
estimates or provider usage where available, cost where available, latency, and
error details. Successful read/list calls such as model discovery do not create
usage rows.

Per-key limits are configured when an operator creates the key:

- Requests per minute
- Tokens per minute

The default key limits are 60 requests/minute and 120,000 tokens/minute.
Consortium also applies a lightweight pre-auth request limiter before API-key
lookup and rejects bearer tokens larger than 4096 bytes.

Rate limiting is process-local. If you run multiple API-serving backend
processes for the same keyspace, put a shared gateway limiter in front of
Consortium.

Errors use an OpenAI-style envelope:

```json
{
  "error": {
    "message": "model route not found",
    "type": "invalid_request_error",
    "param": null,
    "code": "model_not_found"
  }
}
```

Common status classes:

| Status | Typical cause |
| --- | --- |
| `400` | Invalid request or unsupported field. |
| `401` | Missing, invalid, or revoked API key. |
| `404` | Model or stored resource not found for this key. |
| `409` | Idempotency conflict or non-cancellable Response. |
| `413` | Request body too large. |
| `429` | Request or token rate limit exceeded. |
| `5xx` | Provider, workflow, or server failure. |

## Compatibility Matrix

| Capability | Status |
| --- | --- |
| `/v1/models` list/retrieve | Supported |
| Chat Completions text generation | Supported |
| Responses text generation | Supported |
| Stored Chat list/retrieve/messages | Supported with `store:true` |
| Stored Responses retrieve/input_items | Supported for non-streaming/background stored Responses |
| Background Responses | Supported without streaming |
| Chat SSE | Supported, buffered around workflow completion |
| Responses SSE | Supported, buffered around workflow completion |
| `previous_response_id` | Supported for same-key completed stored Responses, up to 20 hops |
| Function tools | Supported for direct-model function tools; hosted tools rejected |
| Structured outputs | Pass-through; `json_schema` requests add provider-parameter guardrails when possible, and model/provider adherence still varies |
| Reasoning effort | Accepted values: `none`, `minimal`, `low`, `medium`, `high`, `xhigh` |
| Prompt cache fields | Accepted as provider/runtime pass-through |
| Metadata | Supported within documented limits |
| `n: 1`, `logprobs: false`, `truncation: "disabled"` | Accepted compatibility values |
| `n > 1`, `logprobs: true`, `top_logprobs > 0`, `prediction` | Rejected |
| Chat audio or non-text modalities | Rejected |
| Deprecated Chat `functions` / `function_call` | Rejected; use `tools` / `tool_choice` |
| Caller-supplied provider routing | Rejected on public `/v1` endpoints |
| Responses `include`, `conversation`, `moderation`, `context_management`, `max_tool_calls` | Rejected |
| Files, Vector Stores, Batches, Embeddings, Moderation endpoint, Realtime, Audio, Images, Video, Fine-tuning, OpenAI org/admin APIs | Out of scope |

## Operator Setup

Operators manage keys, usage, metrics, and model routes with the admin UI or
`conctl`:

```bash
./bin/conctl api keys
./bin/conctl api usage --limit 100
./bin/conctl api usage-export --output usage.csv
./bin/conctl api metrics --stale-minutes 15
./bin/conctl api routes
./bin/conctl api route-upsert \
  --api-model gpt-4o-mini \
  --mode direct_model \
  --provider-model openai/gpt-4o-mini \
  --default \
  --yes
```

Do not expose operator admin endpoints publicly unless they are protected by
trusted authentication. The runtime `/v1` API is authenticated separately with
Consortium API keys.
