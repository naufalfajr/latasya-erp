# Invoice Templates

Invoice templates render data returned by the Invoice module. They do not calculate totals, decide lifecycle transitions, or perform authorization beyond conditionally displaying actions already permitted by the adapter context.

## Fragment contracts

Dynamic invoice lines are rendered by `line_partial.html`. The Invoice pilot will document additional list or form fragments here as they are introduced, including target IDs, triggering routes, and swap behavior.
