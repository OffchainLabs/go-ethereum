## dfuse Fork of `go-ethereum` (`Geth` client)

Here are the instructions on how to work with this repository.

### Initialization

The tooling and other instructions expect the following project
structure, it's easier to work with the dfuse fork when you use
the same names and settings.

```
cd ~/work
git clone git@github.com:eoscanada/go-ethereum-private.git
cd go-ethereum-private

git remote rename origin eoscanada-private
git remote add origin https://github.com/ethereum/go-ethereum.git
git fetch origin

# To ensure that everything is all good
go test ./...
```

### Development

All the development should happen in the `deep-mind` branch, this is our own branch
containing our commits.

When a new version of `Geth` is available, we merge the commits (using the release tag)
into the `deep-mind` branch so we have the latest code. The older deep mind code on the
previous release is tagged with `v<X>-dm` where `X` is the release version.

### Update to New Upstream Version

We are using `v1.9.9` as the example release tag that we want to update to, assuming
`v1.9.7` was the previous latest merged tag. Change those with your own values.

```
git checkout deep-mind
git pull

git tag v1.9.7-dm

git fetch origin
git merge v1.9.9

# Resolves any conflicts, and then `git commit`

go test ./...
git push eoscanada-private deep-mind v1.9.7-dm v1.9.9
```

### View only our commits

**Important** To correctly work, you need to use the right base branch, otherwise, it will be screwed up.

* From `gitk`: `gitk --no-merges --first-parent v1.9.9..deep-mind`
* From terminal: `git log --decorate --pretty=oneline --abbrev-commit --no-merges --first-parent v1.9.9..deep-mind`
* From `GitHub`: [https://github.com/eoscanada/go-ethereum-private/compare/v1.9.9...deep-mind](https://github.com/eoscanada/go-ethereum-private/compare/v1.9.9...deep-mind)
