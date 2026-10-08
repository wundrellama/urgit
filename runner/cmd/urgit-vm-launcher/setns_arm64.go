package main

import "syscall"

// sysSetns is setns(2) on arm64.
const sysSetns = syscall.SYS_SETNS
