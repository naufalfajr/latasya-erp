# Contact module

This module owns customer and supplier records, contact validation, deletion
protection, audit events, and the portal codes used to identify families.

HTTP handlers only translate transport data and module errors. The module also
owns the route list and route-capacity projections used by contact screens; a
contact stores its selected route ID while vehicle assignment remains schema
configuration.
