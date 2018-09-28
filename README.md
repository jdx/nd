[![codecov](https://codecov.io/gh/jdxcode/nd/branch/master/graph/badge.svg)](https://codecov.io/gh/jdxcode/nd)
[![Build Status](https://travis-ci.com/jdxcode/nd.svg?branch=master)](https://travis-ci.com/jdxcode/nd)

What is this?
=============

For example, say you had an express project with a `server.js` script. Instead of running `node ./server.js` you just run `nd ./server.js`. `nd` will just proxy the command to `node`. However if any dependencies specified in `package.json` are not installed or not at the correct version in `package.json` (or `package-lock.json` or `yarn.lock`), `nd` will first fetch them and update the lockfile before running.

How is it so fast?
==================

`nd` is easily the fastest package manager for Node.js projects. On supported filesystems (including APFS on MacOS or btfs on Linux), we take advantage of copy-on-write behavior to create the `node_modules` directory. The're all reflinked from a global cache directory so any other projects on your machine will reuse the same files saving disk space and greatly improving installation time and dramatically reducing disk usage.

If you delete your `node_modules` directory or already have a specific dependency version cached from another project, nd is able to build even a project with many dependencies in under a second.

**No more massive `node_modules` directories for every react project you have.**

This is not the same as hard/symlinks at all (these often cause strange issues with node projects). The files are normal files and you can edit them without impacting other projects. If you make a manual change to a file within `node_modules` (such as debugging with adding a `console.log()` statement), it will create a duplicate the file on the disk when it is altered. This means you can make modifications in one project and it won't mess with others.

This method will be transparent to use as a user of `nd`. It behaves exactly the same as npm or yarn. On filesystems that do not support this we simply fall back to traditonal caching like what npm and yarn does. Even without copy-on-write, nd outperforms yarn and npm.

Plays well with others
======================

Compatible with yarn and npm lockfiles. Does not require any custom config. You can use nd in your team's project without the rest of the team buying into it.

Refresh Flow
============

When you run nd the first thing it does is "refresh" the project. This is analogous to `npm install`, but it's much faster so we do it before we run any script.

The flow follows these steps:

1. `initialize` - read the local `node_modules` tree, `package.json`, lockfile (`yarn.lock` or `package-lock.json`) to check for any missing dependencies or incorrect versions.
2. `resolve` - grab package metadata to find appropriate semver version and tarball url
3. `cache` - download package tarball into global cache directory for use in any project
4. `reflink` - once everything is resolved from step #2, we can now build the ideal `node_modules` tree

Steps 1-3 happen in parallel as soon as `nd`. Meaning that once it finds a missing dependency it immediately starts resolving and caching it while continuing to search for more packages.

Compatibility
=============

Compatibility is a major design goal of `nd`. While the copy-on-write behavior.

* Windows - Unless ReFS becomes more of a thing, copy-on-write behavior is not yet possible.
