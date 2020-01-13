package network

import (
	"sync"
	"time"
)

const (
	maxPoolSize = 200
	connRetries = 3
)

// Discoverer is an interface that is responsible for maintaining
// a healthy connection pool.
type Discoverer interface {
	BackFill(...string)
	PoolCount() int
	RequestRemote(int)
	RegisterBadAddr(string)
	RegisterGoodAddr(string)
	UnregisterConnectedAddr(string)
	UnconnectedPeers() []string
	BadPeers() []string
	GoodPeers() []string
}

// DefaultDiscovery default implementation of the Discoverer interface.
type DefaultDiscovery struct {
	transport        Transporter
	lock             sync.RWMutex
	dialTimeout      time.Duration
	badAddrs         map[string]bool
	connectedAddrs   map[string]bool
	goodAddrs        map[string]bool
	unconnectedAddrs map[string]int
	requestCh        chan int
	pool             chan string
}

// NewDefaultDiscovery returns a new DefaultDiscovery.
func NewDefaultDiscovery(dt time.Duration, ts Transporter) *DefaultDiscovery {
	d := &DefaultDiscovery{
		transport:        ts,
		dialTimeout:      dt,
		badAddrs:         make(map[string]bool),
		connectedAddrs:   make(map[string]bool),
		goodAddrs:        make(map[string]bool),
		unconnectedAddrs: make(map[string]int),
		requestCh:        make(chan int),
		pool:             make(chan string, maxPoolSize),
	}
	go d.run()
	return d
}

// BackFill implements the Discoverer interface and will backfill the
// the pool with the given addresses.
func (d *DefaultDiscovery) BackFill(addrs ...string) {
	d.lock.Lock()
	for _, addr := range addrs {
		if d.badAddrs[addr] || d.connectedAddrs[addr] ||
			d.unconnectedAddrs[addr] > 0 {
			continue
		}
		d.unconnectedAddrs[addr] = connRetries
		d.pushToPoolOrDrop(addr)
	}
	d.lock.Unlock()
}

// PoolCount returns the number of available node addresses.
func (d *DefaultDiscovery) PoolCount() int {
	return len(d.pool)
}

// pushToPoolOrDrop tries to push address given into the pool, but if the pool
// is already full, it just drops it
func (d *DefaultDiscovery) pushToPoolOrDrop(addr string) {
	select {
	case d.pool <- addr:
		updatePoolCountMetric(d.PoolCount())
		// ok, queued
	default:
		// whatever
	}
}

// RequestRemote tries to establish a connection with n nodes.
func (d *DefaultDiscovery) RequestRemote(n int) {
	d.requestCh <- n
}

// RegisterBadAddr registers the given address as a bad address.
func (d *DefaultDiscovery) RegisterBadAddr(addr string) {
	d.lock.Lock()
	d.unconnectedAddrs[addr]--
	if d.unconnectedAddrs[addr] > 0 {
		d.pushToPoolOrDrop(addr)
	} else {
		d.badAddrs[addr] = true
		delete(d.unconnectedAddrs, addr)
	}
	d.lock.Unlock()
}

// UnconnectedPeers returns all addresses of unconnected addrs.
func (d *DefaultDiscovery) UnconnectedPeers() []string {
	d.lock.RLock()
	addrs := make([]string, 0, len(d.unconnectedAddrs))
	for addr := range d.unconnectedAddrs {
		addrs = append(addrs, addr)
	}
	d.lock.RUnlock()
	return addrs
}

// BadPeers returns all addresses of bad addrs.
func (d *DefaultDiscovery) BadPeers() []string {
	d.lock.RLock()
	addrs := make([]string, 0, len(d.badAddrs))
	for addr := range d.badAddrs {
		addrs = append(addrs, addr)
	}
	d.lock.RUnlock()
	return addrs
}

// GoodPeers returns all addresses of known good peers (that at least once
// succeeded handshaking with us).
func (d *DefaultDiscovery) GoodPeers() []string {
	d.lock.RLock()
	addrs := make([]string, 0, len(d.goodAddrs))
	for addr := range d.goodAddrs {
		addrs = append(addrs, addr)
	}
	d.lock.RUnlock()
	return addrs
}

// RegisterGoodAddr registers good known connected address that passed
// handshake successfully.
func (d *DefaultDiscovery) RegisterGoodAddr(s string) {
	d.lock.Lock()
	d.goodAddrs[s] = true
	d.lock.Unlock()
}

// UnregisterConnectedAddr tells discoverer that this address is no longer
// connected, but it still is considered as good one.
func (d *DefaultDiscovery) UnregisterConnectedAddr(s string) {
	d.lock.Lock()
	delete(d.connectedAddrs, s)
	d.lock.Unlock()
}

// registerConnectedAddr tells discoverer that given address is now connected.
func (d *DefaultDiscovery) registerConnectedAddr(addr string) {
	d.lock.Lock()
	delete(d.unconnectedAddrs, addr)
	d.connectedAddrs[addr] = true
	d.lock.Unlock()
}

func (d *DefaultDiscovery) tryAddress(addr string) {
	if err := d.transport.Dial(addr, d.dialTimeout); err != nil {
		d.RegisterBadAddr(addr)
		d.RequestRemote(1)
	} else {
		d.registerConnectedAddr(addr)
	}
}

// run is a goroutine that makes DefaultDiscovery process its queue to connect
// to other nodes.
func (d *DefaultDiscovery) run() {
	var requested int

	for {
		for requested = <-d.requestCh; requested > 0; requested-- {
			select {
			case r := <-d.requestCh:
				if requested <= r {
					requested = r + 1
				}
			case addr := <-d.pool:
				d.lock.RLock()
				addrIsConnected := d.connectedAddrs[addr]
				d.lock.RUnlock()
				updatePoolCountMetric(d.PoolCount())
				if !addrIsConnected {
					go d.tryAddress(addr)
				}
			}
		}
	}
}
