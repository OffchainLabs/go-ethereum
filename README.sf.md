## StreamingFast Firehose Fork of `Ethereum` (`geth` client)

This is our Firehose instrumented fork of [ethereum/go-ethereum](https://github.com/ethereum/go-ethereum) repository. In this README, you will find instructions about how to work with this repository.

### Initialization

The tooling and other instructions expect the following project
structure, it's easier to work with the Firehose fork when you use
the same names and settings.

```
cd ~/work
git clone --branch="deep-mind" git@github.com:streamingfast/go-ethereum.git
cd go-ethereum

git remote rename origin sf

git remote add origin https://github.com/ethereum/go-ethereum.git
git remote add polygon https://github.com/maticnetwork/bor.git
git remote add bsc https://github.com/binance-chain/bsc.git
git remote add heco https://github.com/HuobiGroup/huobi-eco-chain.git
git remote add optimism https://github.com/ethereum-optimism/go-ethereum.git
git remote add fantom-geth https://github.com/Fantom-foundation/go-ethereum.git

git fetch origin
git fetch polygon
git fetch bsc
git fetch heco
git fetch optimism
git fetch fantom-geth

git checkout release/geth-1.9.x-dm
git checkout release/geth-1.10.x-dm
git checkout release/bsc-1.0.x-dm
git checkout release/polygon-0.2.x-dm
git checkout release/heco-1.0.x-dm
git checkout release/optimism-0.1.x-dm
git checkout release/fantom-geth-1.9.x-dm
```

##### Assumptions

For the best result when working with this repository and the scripts it contains:

- The remote `sf` exists on main module and points to `git@github.com:streamingfast/go-ethereum.git`
- The remote `origin` exists on main module and points to https://github.com/ethereum/go-ethereum.git

### Branches & Workflow

Dealing with a big repository like Ethereum that have multiple versions for which we need
to track multiple forks (`Matic`, `BSC`) pose a branch management challenges.

Even more that we have our own set of patches to enable deep data extraction
for Firehose consumption.

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

- `deep-mind` - Default branch with all Firehose Deep Mind commits in it, based on Geth `1.9.10`.
- [release/geth-1.9.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/geth-1.9.x-dm) - Ethereum Geth, latest update for this branch is `1.9.25` ([https://github.com/ethereum/go-ethereum](https://github.com/ethereum/go-ethereum)).
- [release/geth-1.10.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/geth-1.10.x-dm) - Ethereum Geth, latest update for this branch is `1.10.6` ([https://github.com/ethereum/go-ethereum](https://github.com/ethereum/go-ethereum)).
- [release/polygon-0.2.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/polygon-0.2.x-dm) - Polygon fork (a.k.a Matic), based on Geth `1.10.1`, latest update for this branch is `v0.2.5` ([https://github.com/maticnetwork/bor](https://github.com/maticnetwork/bor)).
- [release/bsc-1.0.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/bsc-1.0.x-dm) - BSC fork (Binance), based on Geth `1.9.13`, latest update for this branch is `v1.0.7-ht.3` ([https://github.com/binance-chain/bsc](https://github.com/binance-chain/bsc)).
- [release/heco-1.0.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/heco-1.0.x-dm) - HECO fork (a.k.a Huobi Eco Chain), based on Geth `1.9.23`, latest update for this branch is `v1.0.0` ([https://github.com/HuobiGroup/huobi-eco-chain](https://github.com/HuobiGroup/huobi-eco-chain)).
- [release/optimism-0.1.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/optimism-0.1.x-dm) - Optimism fork, based on Geth `1.9.10`, latest update for this branch is `v0.1.4` ([https://github.com/ethereum-optimism/go-ethereum](https://github.com/ethereum-optimism/go-ethereum)).
- [release/fantom-geth-1.9.x-dm](https://github.com/streamingfast/go-ethereum/tree/release/fantom-geth-1.9.x-dm) - Fantom Geth fork, based on Geth `1.9.22`, latest update for this branch is `v1.9.22-ftm-0.5` (a branch) ([https://github.com/Fantom-foundation/go-ethereum](https://github.com/Fantom-foundation/go-ethereum)).

*Note* To find on which Geth version a particular fork is, you can do `git merge-base sf/release/heco-v1.0.x-dm origin/master` where `origin/master` is the `master` branch of the original Geth repository (https://github.com/ethereum/go-ethereum).

#### Making New Firehose Deep Mind Changes

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

    git push sf deep-mind release/geth-1.9.x-dm release/polygon-0.2.x-dm

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

    git push sf release/geth-1.9.x-dm geth-v1.9.25-dm

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
* From `GitHub`: [https://github.com/streamingfast/go-ethereum/compare/v1.9.23...deep-mind](https://github.com/streamingfast/go-ethereum/compare/v1.9.23...deep-mind)

* Modified files in our fork: `git diff --name-status v1.9.23..deep-mind | grep -E "^M" | cut -d $'\t' -f 2`
