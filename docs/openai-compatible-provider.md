# OpenAI-Compatible Provider

Consortium can use an OpenAI-compatible Chat Completions endpoint as an LLM backend without requiring OpenRouter. This is intended for local and self-hosted runtimes such as Ollama, LM Studio, and vLLM, and also works with hosted services that implement the same request shape.

The compatibility provider is additive: OpenRouter and the compatible endpoint may be enabled at the same time.

## Configuration

Set an API root that contains both `/models` and `/chat/completions`:

```bash
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:11434/v1
```

If the endpoint requires a bearer token:

```bash
OPENAI_COMPATIBLE_API_KEY=<token>
```

Many local endpoints do not require a token, so the key is optional.

At startup Consortium requests `GET $OPENAI_COMPATIBLE_BASE_URL/models`. If that endpoint is unavailable, provide a fallback model list:

```bash
OPENAI_COMPATIBLE_MODELS=qwen3:8b,llama3.2:3b
```

The fallback list is also merged with a successful `/models` response, which is useful for compatible servers whose catalog endpoint intentionally hides some routable models.

## Model namespace

Compatibility-endpoint models are exposed inside Consortium with a `compatible/` prefix:

```text
upstream:    qwen3:8b
Consortium:  compatible/qwen3:8b
```

The prefix prevents an ambiguous registry lookup when the same upstream model name is also available through another provider. Consortium strips `compatible/` before sending the request upstream.

Workflow nodes therefore use the public Consortium model ID:

```json
{
  "type": "prompt",
  "model": "compatible/qwen3:8b",
  "prompt": "Summarize the tradeoffs.",
  "max_tokens": 512,
  "timeout_seconds": 120
}
```

## Ollama

Start Ollama and make sure the model is available:

```bash
ollama pull qwen3:8b
```

Then configure Consortium:

```bash
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:11434/v1
OPENAI_COMPATIBLE_API_KEY=
```

Ollama's OpenAI-compatible `/v1/models` endpoint can normally provide the model catalog automatically. If the runtime or proxy in front of it does not expose `/models`, set `OPENAI_COMPATIBLE_MODELS` explicitly.

## LM Studio

Enable LM Studio's local OpenAI-compatible server, then point Consortium at its `/v1` root. A typical local configuration is:

```bash
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:1234/v1
OPENAI_COMPATIBLE_API_KEY=
```

Use the model ID returned by `GET /v1/models`, prefixed with `compatible/` inside Consortium.

## vLLM

For a vLLM OpenAI-compatible server:

```bash
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:8000/v1
OPENAI_COMPATIBLE_API_KEY=
```

If the endpoint is remote or protected, set `OPENAI_COMPATIBLE_API_KEY` to the token expected by that server.

## Request compatibility

Consortium forwards the standard Chat Completions fields it already models:

- `model`
- `messages`
- `max_tokens`
- `temperature`
- `top_p`
- `stop`
- `response_format`
- `seed`
- `tools`
- `tool_choice`
- `parallel_tool_calls`

OpenRouter-specific controls such as provider routing, OpenRouter reasoning configuration, session IDs, and OpenRouter metadata headers are intentionally not forwarded to a generic endpoint. This avoids leaking provider-specific request fields into local servers that reject unknown parameters.

Tool calls, finish reason, request ID, reasoning fields when returned, and standard token usage are normalized into Consortium's existing `CompletionResponse` shape.

## Errors and retries

The compatibility provider maps HTTP failures into Consortium's existing structured provider errors, including:

- authentication failures
- model-not-found failures
- rate limits and `Retry-After`
- upstream 5xx failures
- gateway/request timeouts
- malformed or empty provider responses

As with OpenRouter, transient network-level failures may be retried inside the provider transport while HTTP retry policy remains the responsibility of the workflow runtime.

## Cost accounting

If the compatible endpoint returns `usage.cost`, Consortium preserves it and the normal accounting path uses that provider-reported value.

The standard OpenAI `/models` response does not include pricing. Consortium therefore does **not** invent input/output prices for dynamically discovered compatibility models. Local/self-hosted models will commonly appear with zero provider cost; operators using a paid compatibility service should treat cost as unavailable unless that endpoint reports it.

## Running without OpenRouter

OpenRouter is no longer required when the compatibility provider is configured. A local-only development setup can therefore be:

```bash
OPENROUTER_API_KEY=
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:11434/v1
make dev
```

At least one provider must be configured. If neither `OPENROUTER_API_KEY` nor `OPENAI_COMPATIBLE_BASE_URL` is set, Consortium refuses to start rather than running with an unusable model registry.
