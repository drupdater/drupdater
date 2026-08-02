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
# The checks come in two kinds. The $expect fields are per fixture: only the
# fixture knows what it was built to produce. Everything after them holds for
# *any* report -- they are the report's own internal consistency, need no
# fixture knowledge, and are therefore always on.
#
# $expect fields, all optional except status:
#   status          list of acceptable .status values
#   min_packages    minimum number of package changes
#   phases          phase names that must be present AND ok
#   addons          keys that must appear under .addons
#   packages_match  regexes, each of which some package name must match
#   forbid_actions  package actions that must not appear at all
#   audit_fixed_min minimum number of advisories composer_audit closed
#
# Secrets are read from the DRUPDATER_ASSERT_SECRETS environment variable,
# newline separated, and asserted to appear nowhere in the document. Unset means
# the check is skipped.

# The phases in the order the workflow runs them. Used to check that the phases
# a run recorded are ordered and unique -- a report whose phases are shuffled or
# repeated describes a run nobody can reason about, whatever .status says.
def phase_order: [
  "acquire working copy",
  "preflight",
  "composer install",
  "baseline site install",
  "update shared code",
  "site update",
  "render merge request",
  "publish"
];

# Addons that both report structured data and render a section into the merge
# request description, with the heading that section starts with.
#
# The two have to agree: an addon that told the report it did something while
# leaving the description silent about it produces a merge request whose
# reviewer is never told what changed. Only addons that render a section belong
# here -- code_beautifier, deprecations_remover and translations_updater report
# without contributing one, and are deliberately absent.
def addon_sections: {
  "composer_audit":      "## 🛡️ Security Report",
  "composer_patches":    "## 🩹 Patch updates",
  "unsupported_modules": "## ⚠️ Unsupported modules",
  "update_hooks":        "## 📄 Job Logs"
};

# An advisory's identity, for comparing the fixed and remaining lists. Mirrors
# the key composer_audit itself uses: CVE first, then the advisory id, then the
# package and title for an advisory carrying neither.
def advisory_key: "\(.cve // "")|\(.advisoryId // "")|\(.packageName // "")|\(.title // "")";

def secrets: (env.DRUPDATER_ASSERT_SECRETS // "") | split("\n") | map(select(length > 0));

. as $r
| ($r.addons // {}) as $addons
| ($r.phases // []) as $phases
| ($r.packages // []) as $packages
| ($r.merge_request_description // "") as $description
| [
  # ---------------------------------------------------------------- $expect

  ( if ($expect.status | index($r.status)) == null
    then "status: got \"\($r.status)\", want one of \($expect.status | join(", "))"
       + (if $r.failed_phase
          then " (failed_phase=\($r.failed_phase): \($r.error // ""))"
          else "" end)
    else empty end ),

  ( if ($packages | length) < ($expect.min_packages // 0)
    then "packages: got \($packages | length), want at least \($expect.min_packages)"
    else empty end ),

  ( ($expect.phases // [])[]
    | . as $name
    | ($phases | map(select(.name == $name))) as $found
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

  # A fixture built around a particular package -- core held a minor behind,
  # say -- only exercises what it is for if that package actually moved. A
  # count alone is satisfied by any unrelated transitive bump.
  ( ($expect.packages_match // [])[]
    | . as $pattern
    | if ($packages | map(select(.package | test($pattern))) | length) == 0
      then "packages: nothing matching /\($pattern)/ was updated"
      else empty end ),

  ( ($expect.forbid_actions // [])[]
    | . as $action
    | ($packages | map(select(.action == $action)) | map(.package)) as $found
    | if ($found | length) > 0
      then "packages: \($action) is not expected here: \($found | join(", "))"
      else empty end ),

  # "composer_audit reported something" is satisfied by a run that closed no
  # advisory at all and only listed the ones still open -- the one outcome a
  # security run must not produce quietly.
  ( (($addons.composer_audit.fixed) // []) as $fixed
    | if $expect.audit_fixed_min and ($fixed | length) < $expect.audit_fixed_min
      then "composer_audit: fixed \($fixed | length) advisories, want at least \($expect.audit_fixed_min)"
      else empty end ),

  # ------------------------------------------------------------ invariants

  ( if $r.schema_version != 1
    then "schema_version: got \($r.schema_version), want 1 -- the report contract changed"
    else empty end ),

  # An empty composer_version means the lookup broke silently, leaving the next upstream
  # composer regression unattributable -- which is the whole reason the field exists.
  ( if (($r.composer_version // "") | length) == 0
    then "composer_version: empty -- the version lookup failed or was dropped"
    else empty end ),

  # Package changes: shape first, because a malformed entry makes every other
  # statement about .packages meaningless.
  ( $packages[]
    | . as $p
    | select((["Install", "Upgrade", "Downgrade", "Remove"] | index($p.action)) == null)
    | "packages: \($p.package // "(unnamed)") has unknown action \"\($p.action // "")\"" ),

  ( $packages[]
    | select(.action == "Upgrade" or .action == "Downgrade")
    | select(((.from // "") | length) == 0 or ((.to // "") | length) == 0)
    | "packages: \(.package) is a \(.action) without both from and to" ),

  ( $packages[]
    | select(.action == "Install")
    | select(((.to // "") | length) == 0)
    | "packages: \(.package) was installed without a version" ),

  ( $packages[]
    | select(.action == "Remove")
    | select(((.from // "") | length) == 0)
    | "packages: \(.package) was removed without a version" ),

  # One composer update produces at most one operation per package, so a
  # duplicate means the operations were parsed out of composer's output wrong.
  ( $packages
    | group_by(.package)
    | map(select(length > 1))[]
    | "packages: \(.[0].package) appears \(length) times" ),

  # Phases are recorded in the order they ran, once each.
  ( $phases
    | group_by(.name)
    | map(select(length > 1))[]
    | "phases: \(.[0].name) recorded \(length) times" ),

  ( $phases[]
    | . as $phase
    | select((phase_order | index($phase.name)) == null)
    | "phases: unknown phase \"\($phase.name)\" -- phase_order in this script is out of date" ),

  ( ($phases | map(. as $phase | phase_order | index($phase.name)) | map(select(. != null))) as $positions
    | if ($positions | length) > 1 and ($positions != ($positions | sort))
      then "phases: recorded out of order: \($phases | map(.name) | join(" -> "))"
      else empty end ),

  ( $phases[]
    | select((.duration_seconds // 0) < 0)
    | "phases: \(.name) has a negative duration" ),

  # A run cannot be shorter than the phases it is made of. One second of slack
  # absorbs the rounding of eight independently measured durations.
  ( ($phases | map(.duration_seconds // 0) | add // 0) as $sum
    | if $sum > (($r.duration_seconds // 0) + 1)
      then "duration: phases add up to \($sum)s but the run lasted \($r.duration_seconds)s"
      else empty end ),

  # Status has to agree with the phases it summarises, in both directions.
  ( if $r.status == "success"
    then ( ($phases | map(select(.ok | not)) | map(.name)) as $failed
           | if ($failed | length) > 0
             then "status: success, but these phases are not ok: \($failed | join(", "))"
             else empty end ),
         ( if (($r.failed_phase // "") | length) > 0 or (($r.error // "") | length) > 0
           then "status: success, but failed_phase=\($r.failed_phase // "") error=\($r.error // "")"
           else empty end )
    else empty end ),

  ( if $r.status == "failed"
    then ( if (($r.failed_phase // "") | length) == 0
           then "status: failed, but no failed_phase is named"
           else ( ($phases | map(select(.name == $r.failed_phase and (.ok | not)))) as $found
                  | if ($found | length) == 0
                    then "status: failed_phase=\($r.failed_phase) is not recorded as a failed phase"
                    else empty end )
           end )
    else empty end ),

  ( if $r.status == "no_changes" and ($r.merge_request != null)
    then "status: no_changes, but a merge request was created (\($r.merge_request.url // ""))"
    else empty end ),

  # A --dry-run does everything except publish. That it published anyway is the
  # single worst thing this report could be hiding.
  ( if $r.dry_run == true and ($r.merge_request != null)
    then "dry_run: a merge request was created anyway (\($r.merge_request.url // ""))"
    else empty end ),

  ( if $r.merge_request != null and (($r.merge_request.url // "") | length) == 0
    then "merge_request: recorded without a url"
    else empty end ),

  # The branch is named as soon as the code update succeeds -- including under
  # --dry-run, where it is the only handle on what the run produced.
  ( if ($phases | map(select(.name == "update shared code" and .ok)) | length) > 0
      and (($r.update_branch // "") | length) == 0
    then "update_branch: empty although the code update succeeded"
    else empty end ),

  # Same for the merge request's title and description: rendering them is a
  # phase of its own so that a dry run assembles them too.
  ( if ($phases | map(select(.name == "render merge request" and .ok)) | length) > 0
    then ( if (($r.merge_request_title // "") | length) == 0
           then "merge_request_title: empty although the merge request was rendered"
           else empty end ),
         ( if ($description | length) == 0
           then "merge_request_description: empty although the merge request was rendered"
           else empty end )
    else empty end ),

  # What an addon reported and what the reviewer is told must be the same story.
  ( addon_sections
    | to_entries[]
    | . as $section
    | select(($addons | has($section.key)) and ($description | length) > 0)
    | select(($description | contains($section.value)) | not)
    | "merge_request_description: \($section.key) reported data but rendered no \"\($section.value)\" section" ),

  # Site-keyed addon sections can only be about sites the run was configured for.
  ( ["update_hooks", "translations_updater"][]
    | . as $key
    | ($addons[$key] // {})
    | if type == "object"
      then keys[]
           | . as $site
           | select((($r.sites // []) | index($site)) == null)
           | "\($key): reports site \"\($site)\", which is not in .sites"
      else empty end ),

  ( ($addons.unsupported_modules // [])[]
    | select(((.name // "") | length) == 0)
    | "unsupported_modules: an entry has no name" ),

  # An advisory cannot be both closed by this update and still open after it.
  ( (($addons.composer_audit.fixed // []) | map(advisory_key)) as $fixed
    | (($addons.composer_audit.remaining // []) | map(advisory_key)) as $remaining
    | ($fixed - ($fixed - $remaining))[]
    | "composer_audit: advisory \(.) is reported as both fixed and remaining" ),

  # translations_updater exists to make "ran and found nothing" distinguishable
  # from "bailed out early", so every site it reports has to say which it was.
  ( ($addons.translations_updater // {})
    | to_entries[]
    | select((.value.updated != true) and (((.value.skipped // "") | length) == 0))
    | "translations_updater: site \(.key) neither updated nor gave a skip reason" ),

  # The report is archived and attached to tickets. A credential reaching it is
  # a finding regardless of what else the run did right.
  ( ($r | tojson) as $document
    | secrets[]
    | select(. as $secret | $document | contains($secret))
    | "credential: a secret from the environment appears in the report" )
]
| if length == 0 then "report assertions passed"
  else "report assertions failed:\n  - " + join("\n  - ") | halt_error(1)
  end
