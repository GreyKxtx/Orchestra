<api-design-principles>
Designing an API (REST / gRPC / GraphQL / internal package interface) is a contract decision — much harder to change than implementation.

**Principles:**

1. **Resource-oriented.** Model nouns, expose verbs as operations on those nouns. `POST /orders/{id}/cancel` beats `POST /cancelOrder?id=...`.
2. **Consistent naming.** Pick a casing (snake_case for JSON fields is conventional in REST) and stick to it. Mixed conventions are a smell that grows.
3. **Idempotent verbs are idempotent.** `PUT` and `DELETE` must be safe to retry. If they aren't, you've designed them wrong.
4. **Pagination is explicit.** Never return unbounded collections. Cursor-based ≫ offset-based for stable iteration.
5. **Errors are first-class.** Structured error response: `{code, message, details}`. Don't leak stack traces.
6. **Versioning strategy decided upfront.** URL path (`/v1`), header, or content-type. Document the deprecation policy.
7. **Backward compatibility by default.** Additions safe, removals not. Booleans and enums grow — design types that can be extended.

**Avoid:**
- "Generic" endpoints that take a `type` field and switch on it server-side — destroys typed clients.
- Returning different shapes for the "same" endpoint based on params.
- Mixing read and write semantics in one endpoint (`GET` that has side effects).
- Boolean fields that grow into 3-state needs (`active` becomes `active|paused|archived` — use an enum from day 1).
- Field names that encode current implementation (`mysql_user_id` — rename if backend changes).

**Output of a design pass:**
- Endpoint table (method, path, purpose) or interface signature.
- Request/response schemas with example payloads.
- Error catalogue.
- Versioning policy.
- Concrete examples for the 3 happy paths and 2 representative error paths.
- Open questions that need user/architect decision.
</api-design-principles>
