# Function overloads

PostgreSQL tells overloaded functions apart by their argument types, so the
squash must too:

- `fmt(integer)` is replaced (spelled `int4` the second time), `fmt(text)` is
  dropped with its comment, and `fmt(boolean)` is added: the baseline keeps
  the replaced integer version, the boolean one and only the integer comment.
- `COMMENT ON FUNCTION label` is written without arguments while `label` has
  one overload; a later migration adds `label(text)`. The baseline must name
  `label(bigint)` in the comment, or PostgreSQL rejects it as ambiguous.
- The trigger references `touch()` without a signature.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with the catalog of the original history.
