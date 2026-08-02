# Verifies a drupdater run report's package list against the composer.lock it produced.
#
#   jq -n -e -r --slurpfile before old-composer.lock \
#                --slurpfile after  composer.lock \
#                --slurpfile report report.json \
#                -f .github/assert-lock-matches-report.jq
#
# The report's "packages" field is parsed out of composer's console output, not
# read back from the lock, so everything downstream of it -- the dependency
# table in the merge request, a fleet-wide view of what moved, this repository's
# own integration assertions -- trusts a regular expression over human-readable
# text. That regex has to keep up with composer's output, and nothing in the
# unit tests would notice if it stopped doing so: they feed it the output it was
# written against.
#
# Diffing the two lock files is the independent second opinion. It is derived
# from the artefact the run actually committed, so a report that says a package
# moved when the lock disagrees -- or stays silent about one that did -- fails
# here rather than being believed.

# name -> version, across both sections. require-dev packages are updated by the
# same composer update and are as much a part of the diff as the rest.
def packages_map:
  (((.packages // []) + (."packages-dev" // []))
   | map({key: .name, value: (.version // "")})
   | from_entries);

# The report's action vocabulary, reduced to what a lock diff can actually
# distinguish. Upgrade and Downgrade are one case here: telling them apart needs
# a semantic version comparison, which jq has no business doing -- the direction
# is composer's own statement and there is nothing in the lock to check it
# against.
def category: if . == "Upgrade" or . == "Downgrade" then "change" else ascii_downcase end;

($before[0] | packages_map) as $was
| ($after[0] | packages_map) as $now
| ($report[0].packages // []) as $reported
| ($reported | map({key: .package, value: .}) | from_entries) as $by_name
| ( [ ($now | keys_unsorted[] | . as $name
       | select(($was | has($name)) | not)
       | {package: $name, category: "install", to: $now[$name]}),
      ($was | keys_unsorted[] | . as $name
       | select(($now | has($name)) | not)
       | {package: $name, category: "remove", from: $was[$name]}),
      ($now | keys_unsorted[] | . as $name
       | select(($was | has($name)) and ($was[$name] != $now[$name]))
       | {package: $name, category: "change", from: $was[$name], to: $now[$name]})
    ] ) as $expected
| ( $expected | map({key: .package, value: .}) | from_entries ) as $expected_by_name
| [
  # Every lock change has to appear in the report, with the same shape.
  ( $expected[]
    | . as $want
    | ($by_name[$want.package] // null) as $got
    | if $got == null
      then "composer.lock changed \($want.package) (\($want.category)) but the report does not mention it"
      elif ($got.action | category) != $want.category
      then "\($want.package): composer.lock shows a \($want.category), the report says \($got.action)"
      elif ($want.from != null) and (($got.from // "") != $want.from)
      then "\($want.package): composer.lock has it going from \($want.from), the report says \($got.from // "")"
      elif ($want.to != null) and (($got.to // "") != $want.to)
      then "\($want.package): composer.lock has it going to \($want.to), the report says \($got.to // "")"
      else empty end ),

  # ... and nothing may be reported that the lock does not show. A report
  # claiming an update that never landed is the more dangerous direction: it is
  # the one a reviewer would act on.
  ( $reported[]
    | . as $got
    | select(($expected_by_name | has($got.package)) | not)
    | "\($got.package): the report claims a \($got.action) that composer.lock does not show" ),

  # A run that updated nothing has no business reporting packages, and one that
  # changed the lock has no business reporting none.
  ( if ($expected | length) == 0 and ($reported | length) > 0
    then "composer.lock is unchanged, but the report lists \($reported | length) package changes"
    elif ($expected | length) > 0 and ($reported | length) == 0
    then "composer.lock changed \($expected | length) packages, but the report lists none"
    else empty end )
]
| if length == 0
  then "composer.lock matches the report (\($expected | length) package changes)"
  else "composer.lock does not match the report:\n  - " + join("\n  - ") | halt_error(1)
  end
