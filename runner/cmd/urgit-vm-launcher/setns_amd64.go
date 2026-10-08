package main

// sysSetns is setns(2) on x86_64 (the standard library's syscall package
// does not export it for this architecture).
const sysSetns = 308
