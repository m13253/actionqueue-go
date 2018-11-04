# actionqueue-go: A timed event queue with expiry support

[![](https://godoc.org/github.com/m13253/actionqueue-go?status.svg)](http://godoc.org/github.com/m13253/actionqueue-go)

Package actionqueue provides a timed event queue with expiry support.

Any event is added to the Queue with two parameters, ActionTime and ExpireTime.

The Queue will notify the caller on ActionTime through NextAction channel. If
the notification is not received in time, it will persist until ExpireTime
arrives. Other events may only fire up after this event is received or expires.
