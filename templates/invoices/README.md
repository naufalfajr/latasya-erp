# Invoice Templates

Invoice templates render data returned by the Invoice module. They do not calculate totals, decide lifecycle transitions, or perform authorization beyond conditionally displaying actions already permitted by the adapter context.

## Fragment contracts

### Invoice line row

- Template: `line_partial.html`, definition `invoice-line-row`
- Route: `GET /dashboard/htmx/invoice-line`
- Target: `#inv-lines-body`
- Swap: `beforeend`
- Response: one standalone `<tr>` with a default quantity of `1.00` and the active revenue-account options

The full invoice form uses the same definition for existing rows, preventing the initial and dynamically added markup from drifting.

### Delete redirect

The invoice detail page sends `DELETE /dashboard/invoices/{id}` with `hx-target="body"`. A successful HTMX deletion returns an empty `200` response with `HX-Redirect: /dashboard/invoices`; a non-HTMX deletion keeps the existing `303` redirect.
