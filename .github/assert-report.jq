# Verifies a drupdater run report against a fixture's expectations.
#
#   jq -e --argjson expect "$EXPECT" -f .github/assert-report.jq report.json
#
# Gating on .status alone is a smoke test: a run reports success even when an
# addon failed, because most addons log and swallow their own errors. That is
# exactly how unsupported_modules stayed silently broken on Drupal 11 -- every
# run was green while the addon never reported a thing. Asserting that the
# addons a fixture is built to exercise actually appear in the report is what
# turns this job from "it did not crash" into "it did what it is for".
#
# $expect fields, all optional except status:
#   status        list of acceptable .status values
#   min_packages  minimum number of package changes
#   phases        phase names that must be present AND ok
#   addons        keys that must appear under .addons

. as $r
| ($r.addons // {}) as $addons
| [
  ( if ($expect.status | index($r.status)) == null
    then "status: got \"\($r.status)\", want one of \($expect.status | join(", "))"
       + (if $r.failed_phase
          then " (failed_phase=\($r.failed_phase): \($r.error // ""))"
          else "" end)
    else empty end ),

  ( if ($r.packages | length) < ($expect.min_packages // 0)
    then "packages: got \($r.packages | length), want at least \($expect.min_packages)"
    else empty end ),

  ( ($expect.phases // [])[]
    | . as $name
    | ($r.phases | map(select(.name == $name))) as $found
    | if ($found | length) == 0 then "phase missing: \($name)"
      elif ($found | map(select(.ok)) | length) == 0
      then "phase not ok: \($name) (\($found[0].error // "no error recorded"))"
      else empty end ),

  ( ($expect.addons // [])[]
    | . as $key
    | if ($addons | has($key)) then empty
      else "addon reported nothing: \($key) -- present in report: "
         + (($addons | keys | join(", ")) as $k
            | if $k == "" then "(none)" else $k end)
      end ),

  ( if $r.schema_version != 1
    then "schema_version: got \($r.schema_version), want 1 -- the report contract changed"
    else empty end )
]
| if length == 0 then "report assertions passed"
  else "report assertions failed:\n  - " + join("\n  - ") | halt_error(1)
  end
