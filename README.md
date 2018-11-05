# actionqueue-go: A timed event queue with expiry support

[![](https://godoc.org/github.com/m13253/actionqueue-go?status.svg)](http://godoc.org/github.com/m13253/actionqueue-go)

Package actionqueue provides a timed event queue with expiry support.

Any event is added to the queue with two parameters, `ActionTime` and
`ExpireTime`. The event will be fired upon `ActionTime` through the the
`NextEvent` channel.

The caller may choose either to receive an event immediately, or at any time
before it is expired. The queue takes care of the `ExpireTime`, and cancel
expired events upon expiry.
