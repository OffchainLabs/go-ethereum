## dfuse Fork of `Ethereum` (`geth` client)

This is our private instrumented fork of [ethereum/go-ethereum](https://github.com/ethereum/go-ethereum) repository. In this README, you will find instructions about how to work with this repository.

### Initialization

The tooling and other instructions expect the following project
structure, it's easier to work with the dfuse fork when you use
the same names and settings.

    cd ~/work
    git clone --branch="deep-mind" git@github.com:dfuse-io/go-ethereum-private.git
    cd go-ethereum-private

    git remote rename origin dfuse-io-private
    git remote add origin https://github.com/ethereum/go-ethereum
    git fetch origin

##### Assumptions

For the best result when working with this repository and the scripts it contains:

- The remote `dfuse-io-private` exists on main module and points to `git@github.com:dfuse-io/go-ethereum-private.git`
- The remote `origin` exists on main module and points to https://github.com/ethereum/go-ethereum

### Branches & Workflow

Dealing with a big repository like Ethereum that have multiple for which we need
to track multiple forks (`Matic`, `BSC`) pose a branch management challenges.

Even more that we have our own set of patches to enable deep data extraction
for dfuse consumption.

We use merging of the branches into one another to make that work manageable.
The first and foremost important rule is that we always put new development to
deep mind in the `deep-mind` branch.

This branch must always be tracking the lowest supported version of all. Indeed,
this is our "work" branch for our patches, **new development must go there**. If you
perform our work with newer code, the problem that will arise is that this new
deep mind work will not be mergeable into forks or older release that we still
support!

We then create `release/<identifier>` branch that tracks the version of interest
for us, versions that we will manages and deploy.

Currently supported forks & version and the release branch

- `release/geth-1.9.x-dm` - Ethereum geth, latest update for this branch is `1.9.25`.
- `release/polygon-0.2.x-dm` - Polygon fork (a.k.a Matic), latest update for this branch is `0.2.4` (based on Geth `1.9.24`).
- `deep-mind` - based on Geth `1.9.23` version of Ethereum repository, with all dfuse Deep Mind commits in it.

**Note** We are planning some other fork support that might go down to Geth `1.9.13`, hence why we keep the
version to `1.9.23`.

#### Making New Deep Mind Changes

Making new Deep Mind changes should be performed on the `deep-mind` branch. When happy
with the changes, simply merge the `deep-mind` branch in all the release branches we track
and support.

    git checkout deep-mind
    git pull -p

    # Perform necessary changes, tests and commit(s)

    git checkout release/geth-1.9.x-dm
    git pull -p
    git merge deep-mind

    git checkout release/polygon-0.2.x-dm
    git pull -p
    git merge deep-mind

    git push dfuse-io-private deep-mind release/geth-1.9.x-dm release/polygon-0.2.x-dm

### Update to New Upstream Version

We assume you are in the top directory of the repository when performing the following
operations. Here, we outline the rough idea. Extra details and command lines to use
will be completed later if missing.

We are using `v1.9.25` as the example release tag that we want to update to, assuming
`v1.9.23` was the previous latest merged tag. Change
those with your own values.

First step is to checkout the release branch of the series you are currently
updating to:

    git checkout release/geth-1.9.x-dm
    git pull -p

You first fetch the origin repository new data from Git:

    git fetch origin -p

Then apply the update

    git merge v1.9.25

Solve conflicts if any. Once all conflicts have been resolved, commit then
create a tag with release

    git tag geth-v1.9.25-dm

Then push all that to the repository:

    git push dfuse-io-private release/geth-1.9.x-dm geth-v1.9.25-dm

### Development

All the development should happen in the `deep-mind` branch, this is our own branch
containing our commits.

##### Build Locally

    go install ./cmd/geth

#### Release

TBC

### View only our commits

**Important** To correctly work, you need to use the right base branch, otherwise, it will be screwed up. The `deep-mind`
branch was based on `v1.9.23` at time of writing.

* From `gitk`: `gitk --no-merges --first-parent v1.9.23..deep-mind`
* From terminal: `git log --decorate --pretty=oneline --abbrev-commit --no-merges --first-parent v1.9.23..deep-mind`
* From `GitHub`: [https://github.com/dfuse-io/go-ethereum-private/compare/v1.9.23...deep-mind](https://github.com/dfuse-io/go-ethereum-private/compare/v1.9.23...deep-mind)

* Modified files in our fork: `git diff --name-status v1.9.23..deep-mind | grep -E "^M" | cut -d $'\t' -f 2`
