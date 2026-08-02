# JSON API Adapters

The JSON API is a transport adapter over shared business modules. It owns HTTP authentication, request decoding, response encoding, pagination representation, idempotency middleware, and OpenAPI compatibility.

Business validation, authorization, lifecycle decisions, accounting effects, and transaction behavior belong to business modules. JSON handlers translate typed module errors into stable HTTP statuses and error codes.

The canonical external contract is [`api/openapi.yaml`](../../api/openapi.yaml). Contract tests must pass whenever an adapter changes.
