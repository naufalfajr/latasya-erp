# Document number module

`documentnumber` atomically claims monthly invoice, bill, credit-note, and
journal reference sequences from SQLite. Callers choose a typed document kind;
the module alone owns schema targets, prefixes, and sequence seeding.
