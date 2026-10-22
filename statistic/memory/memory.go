package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/statistic"
)

const Name = "MEMORY"

type User struct {
	// WARNING: do not change the order of these fields.
	// 64-bit fields that use `sync/atomic` package functions
	// must be 64-bit aligned on 32-bit systems.
	// Reference: https://github.com/golang/go/issues/599
	// Solution: https://github.com/golang/go/issues/11891#issuecomment-433623786
	sent      uint64
	recv      uint64
	lastSent  uint64
	lastRecv  uint64
	sendSpeed uint64
	recvSpeed uint64

	hash        string
	ipTable     sync.Map
	ipNum       int32
	maxIPNum    int
	limiterLock sync.RWMutex
	sendLimiter *rate.Limiter
	recvLimiter *rate.Limiter
	// limiterConfigured is set once a limiter has ever been assigned and
	// never cleared afterwards: users without any speed limit skip the
	// limiter lock entirely in AddTraffic, and a user whose limit was removed
	// keeps taking the (cheap, uncontended) locked path harmlessly.
	limiterConfigured atomic.Bool
	ctx               context.Context
	cancel            context.CancelFunc
}

func (u *User) Close() error {
	u.ResetTraffic()
	u.cancel()
	return nil
}

func (u *User) AddIP(ip string) bool {
	if u.maxIPNum <= 0 {
		return true
	}
	_, found := u.ipTable.Load(ip)
	if found {
		return true
	}
	if int(u.ipNum)+1 > u.maxIPNum {
		return false
	}
	u.ipTable.Store(ip, true)
	atomic.AddInt32(&u.ipNum, 1)
	return true
}

func (u *User) DelIP(ip string) bool {
	if u.maxIPNum <= 0 {
		return true
	}
	_, found := u.ipTable.Load(ip)
	if !found {
		return false
	}
	u.ipTable.Delete(ip)
	atomic.AddInt32(&u.ipNum, -1)
	return true
}

func (u *User) GetIP() int {
	return int(u.ipNum)
}

func (u *User) SetIPLimit(n int) {
	u.maxIPNum = n
}

func (u *User) GetIPLimit() int {
	return u.maxIPNum
}

func (u *User) AddTraffic(sent, recv int) {
	// Fast path: most users never have a speed limit configured, so skip the
	// limiter lock entirely. The flag is only ever set while holding the
	// write lock together with the limiter pointers, so once it is observed
	// the RLock below is guaranteed to see the pointers.
	if u.limiterConfigured.Load() {
		u.limiterLock.RLock()
		sendLimiter := u.sendLimiter
		recvLimiter := u.recvLimiter
		u.limiterLock.RUnlock()
		if sendLimiter != nil && sent >= 0 {
			sendLimiter.WaitN(u.ctx, sent)
		}
		if recvLimiter != nil && recv >= 0 {
			recvLimiter.WaitN(u.ctx, recv)
		}
	}
	atomic.AddUint64(&u.sent, uint64(sent))
	atomic.AddUint64(&u.recv, uint64(recv))
}

func (u *User) SetSpeedLimit(send, recv int) {
	u.limiterLock.Lock()
	defer u.limiterLock.Unlock()

	if send <= 0 {
		u.sendLimiter = nil
	} else {
		u.sendLimiter = rate.NewLimiter(rate.Limit(send), send*2)
		u.limiterConfigured.Store(true)
	}
	if recv <= 0 {
		u.recvLimiter = nil
	} else {
		u.recvLimiter = rate.NewLimiter(rate.Limit(recv), recv*2)
		u.limiterConfigured.Store(true)
	}
}

func (u *User) GetSpeedLimit() (send, recv int) {
	u.limiterLock.RLock()
	defer u.limiterLock.RUnlock()

	if u.sendLimiter != nil {
		send = int(u.sendLimiter.Limit())
	}
	if u.recvLimiter != nil {
		recv = int(u.recvLimiter.Limit())
	}
	return
}

func (u *User) Hash() string {
	return u.hash
}

func (u *User) SetTraffic(send, recv uint64) {
	atomic.StoreUint64(&u.sent, send)
	atomic.StoreUint64(&u.recv, recv)
}

func (u *User) GetTraffic() (uint64, uint64) {
	return atomic.LoadUint64(&u.sent), atomic.LoadUint64(&u.recv)
}

func (u *User) ResetTraffic() (uint64, uint64) {
	sent := atomic.SwapUint64(&u.sent, 0)
	recv := atomic.SwapUint64(&u.recv, 0)
	atomic.StoreUint64(&u.lastSent, 0)
	atomic.StoreUint64(&u.lastRecv, 0)
	return sent, recv
}

func (u *User) speedUpdater() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-u.ctx.Done():
			return
		case <-ticker.C:
			sent, recv := u.GetTraffic()
			// lastSent/lastRecv must be read atomically: ResetTraffic
			// (invoked via Close) stores them concurrently.
			atomic.StoreUint64(&u.sendSpeed, sent-atomic.LoadUint64(&u.lastSent))
			atomic.StoreUint64(&u.recvSpeed, recv-atomic.LoadUint64(&u.lastRecv))
			atomic.StoreUint64(&u.lastSent, sent)
			atomic.StoreUint64(&u.lastRecv, recv)
		}
	}
}

func (u *User) GetSpeed() (uint64, uint64) {
	return atomic.LoadUint64(&u.sendSpeed), atomic.LoadUint64(&u.recvSpeed)
}

type Authenticator struct {
	users sync.Map
	ctx   context.Context
}

func (a *Authenticator) AuthUser(hash string) (bool, statistic.User) {
	if user, found := a.users.Load(hash); found {
		return true, user.(*User)
	}
	return false, nil
}

func (a *Authenticator) AddUser(hash string) error {
	if _, found := a.users.Load(hash); found {
		return common.NewError("hash " + hash + " is already exist")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	meter := &User{
		hash:   hash,
		ctx:    ctx,
		cancel: cancel,
	}
	go meter.speedUpdater()
	a.users.Store(hash, meter)
	return nil
}

func (a *Authenticator) DelUser(hash string) error {
	meter, found := a.users.Load(hash)
	if !found {
		return common.NewError("hash " + hash + " not found")
	}
	meter.(*User).Close()
	a.users.Delete(hash)
	return nil
}

func (a *Authenticator) ListUsers() []statistic.User {
	result := make([]statistic.User, 0)
	a.users.Range(func(k, v interface{}) bool {
		result = append(result, v.(*User))
		return true
	})
	return result
}

func (a *Authenticator) Close() error {
	return nil
}

func NewAuthenticator(ctx context.Context) (statistic.Authenticator, error) {
	cfg := config.FromContext(ctx, Name).(*Config)
	u := &Authenticator{
		ctx: ctx,
	}
	for _, password := range cfg.Passwords {
		hash := common.SHA224String(password)
		u.AddUser(hash)
	}
	log.Debug("memory authenticator created")
	return u, nil
}

func init() {
	statistic.RegisterAuthenticatorCreator(Name, NewAuthenticator)
}
