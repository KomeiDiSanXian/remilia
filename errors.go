package remilia

// Deprecated: errors.go used to mix framework execution errors and generic utilities.
// Implementations have been split into:
//   - handler_error.go (HandlerError/BlockError/WrapError/FormatHandlerError/DLQ JSON)
//   - errors_stack.go (stack trace toggles & capture)
//   - errors_util.go (predefined errors and generic error helpers)
//
// This file is intentionally left as a shim to keep repository history and external
// references stable.
