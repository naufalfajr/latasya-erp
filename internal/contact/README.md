# Contact module

This module owns customer and supplier records, contact validation, deletion
protection, audit events, and the portal codes used to identify families.

HTTP handlers only translate transport data and module errors. The module also
owns the route list and route-capacity projections used by contact screens; a
contact stores its selected route ID while vehicle assignment remains schema
configuration.

The module also owns transport pricing from distance, sibling discount, and
return-only attributes; invoice generation consumes that calculation.

Portal-code creation is a privileged mutation. Cross-module workflows must pass
an actor with portal-management authority; reads by an existing public code do
not require an actor.
