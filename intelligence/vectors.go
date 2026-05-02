package intelligence

// CanonicalFields is the reference set of normalized API field names.
// Vectors are computed at runtime using character n-gram hashing.
var CanonicalFields = []string{
        "id", "uid", "uuid", "user_id", "account_id", "customer_id",
        "email", "email_address",
        "name", "first_name", "last_name", "full_name", "display_name", "username",
        "phone", "phone_number", "mobile",
        "address", "street", "city", "state", "country", "zip", "postal_code",
        "created_at", "updated_at", "deleted_at", "timestamp", "date", "time",
        "status", "state", "active", "enabled", "verified", "confirmed",
        "error", "message", "description", "reason", "detail",
        "token", "access_token", "refresh_token", "api_key", "secret",
        "key", "value", "data", "result", "results", "items", "records", "list", "response", "payload",
        "url", "link", "href", "endpoint", "path",
        "type", "kind", "category", "tag", "label",
        "count", "total", "size", "length", "limit", "offset", "page",
        "price", "amount", "cost", "fee", "currency",
        "image", "avatar", "photo", "thumbnail",
        "title", "subject", "body", "content", "text",
        "role", "permission", "scope", "group",
        "order_id", "product_id", "item_id", "transaction_id",
        "metadata", "attributes", "properties", "settings", "config",
        "version", "revision", "checksum", "hash",
        "parent_id", "child_id", "owner_id", "creator_id",
        "source", "destination", "origin", "target",
        "method", "action", "operation", "event",
        "code", "number", "reference", "identifier",
}

// VectorDim is the number of dimensions in computed embeddings.
const VectorDim = 128
