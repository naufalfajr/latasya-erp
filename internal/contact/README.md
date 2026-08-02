# Contact module

This module owns customer and supplier records, contact validation, deletion
protection, audit events, and the portal codes used to identify families.

HTTP handlers only translate transport data and module errors. Route scheduling
and capacity remain outside this module; a contact stores only its selected
route ID.
