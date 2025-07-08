package pwcache

import (
	"log"
	"reflect"

	"github.com/Sam-Yang6/pwcache/pwqueue"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/akita/v3/tracing"
)

// A PWC is a cache that maintains some page information.
type PWC struct {
	*sim.TickingComponent

	topPort     sim.Port
	bottomPort  sim.Port
	controlPort sim.Port
	LowModule   sim.Port

	numSets        int
	numWays        int
	pageSize       uint64
	numReqPerCycle int
	log2PageSize   uint64

	Sets []Set

	mshr                mshr
	respondingMSHREntry *mshrEntry
	pwqueue             *pwqueue.PWQueue

	isPaused bool
}

// Reset sets all the entries int he PWC to be invalid
func (pwc *PWC) reset() {
	pwc.Sets = make([]Set, pwc.numSets)
	for i := 0; i < pwc.numSets; i++ {
		set := NewSet(pwc.numWays)
		pwc.Sets[i] = set
	}
}

// Tick defines how PWC update states at each cycle
func (pwc *PWC) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = pwc.performCtrlReq(now) || madeProgress

	if !pwc.isPaused {
		for i := 0; i < pwc.numReqPerCycle; i++ {
			madeProgress = pwc.respondMSHREntry(now) || madeProgress
		}

		for i := 0; i < pwc.numReqPerCycle; i++ {
			madeProgress = pwc.MSHRlookup(now) || madeProgress
		}

		for i := 0; i < 8; i++ { //GMMU 8个page table walker
			madeProgress = pwc.PWClookup(now, i) || madeProgress
		}

		for i := 0; i < pwc.numReqPerCycle; i++ {
			madeProgress = pwc.parseBottom(now) || madeProgress
		}
	}

	return madeProgress
}

func (pwc *PWC) respondMSHREntry(now sim.VTimeInSec) bool { //正返回的mshr表项
	if pwc.respondingMSHREntry == nil {
		return false
	}

	mshrEntry := pwc.respondingMSHREntry
	page := mshrEntry.page
	req := mshrEntry.Requests[0]
	rspToTop := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(pwc.topPort).
		WithDst(req.Src).
		WithRspTo(req.ID).
		WithPage(page).
		Build()
	err := pwc.topPort.Send(rspToTop)
	if err != nil {
		return false
	}

	mshrEntry.Requests = mshrEntry.Requests[1:]
	if len(mshrEntry.Requests) == 0 {
		pwc.respondingMSHREntry = nil
	}

	tracing.TraceReqComplete(req, pwc)
	return true
}

func (pwc *PWC) MSHRlookup(now sim.VTimeInSec) bool { //在mshr中查找
	msg := pwc.topPort.Peek()
	if msg == nil {
		return false
	}

	req := msg.(*vm.TranslationReq)

	mshrEntry := pwc.mshr.Query(req.PID, req.VAddr) //在mshr中查找
	if mshrEntry != nil {                           //如果找到了
		return pwc.processPWCMSHRHit(now, mshrEntry, req) //处理mshr命中
	} else {
		return pwc.processPWCMSHRMISS(now, req)
	}
}
func (pwc *PWC) PWClookup(now sim.VTimeInSec, i int) bool {
	pwe, err := pwc.pwqueue.Index(i)
	if err != nil {
		return false
	}

	if pwe.Cyclesleft != 0 { //未达到pwc的访问延迟
		pwe.Cyclesleft--
		return true
	}

	if pwe.Inpwcache { //已经进入pwcache
		return false
	}

	pwe.Inpwcache = true
	req := pwe.Req
	l4index := req.VAddr >> (pwc.log2PageSize + 27) << (pwc.log2PageSize + 27)
	l3index := req.VAddr >> (pwc.log2PageSize + 18) << (pwc.log2PageSize + 18)
	l2index := req.VAddr >> (pwc.log2PageSize + 9) << (pwc.log2PageSize + 9)

	setID := pwc.vAddrToSetID(l2index) //计算setID
	set := pwc.Sets[setID]
	wayID, _, foundl2 := set.Lookup(req.PID, l2index) //在set中查找

	if foundl2 {
		pwc.visit(setID, wayID)
		pwc.pwqueue.Updatehitl(i, 3)
		tracing.TraceReqReceive(req, pwc)
		tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "hit")
		_ = pwc.fetchBottom(now, req, 3)
		return true
	}

	setID = pwc.vAddrToSetID(l3index) //计算setID
	set = pwc.Sets[setID]
	wayID, _, foundl3 := set.Lookup(req.PID, l3index) //在set中查找

	if foundl3 {
		pwc.visit(setID, wayID)
		pwc.pwqueue.Updatehitl(i, 2)
		tracing.TraceReqReceive(req, pwc)
		tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "hit")
		_ = pwc.fetchBottom(now, req, 2)
		return true
	}

	setID = pwc.vAddrToSetID(l4index) //计算setID
	set = pwc.Sets[setID]
	wayID, _, foundl4 := set.Lookup(req.PID, l4index) //在set中查找

	if foundl4 {
		pwc.visit(setID, wayID)
		pwc.pwqueue.Updatehitl(i, 1)
		tracing.TraceReqReceive(req, pwc)
		tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "hit")
		_ = pwc.fetchBottom(now, req, 1)
		return true
	}
	pwc.pwqueue.Updatehitl(i, 0)
	tracing.TraceReqReceive(req, pwc)
	tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "miss")
	_ = pwc.fetchBottom(now, req, 0)
	return true
}

func (pwc *PWC) vAddrToSetID(vAddr uint64) (setID int) {
	return int(vAddr / pwc.pageSize % uint64(pwc.numSets))
}

func (pwc *PWC) processPWCMSHRHit( //处理MSHR命中
	now sim.VTimeInSec,
	mshrEntry *mshrEntry,
	req *vm.TranslationReq,
) bool {

	mshrEntry.Requests = append(mshrEntry.Requests, req)

	pwc.topPort.Retrieve(now)
	tracing.TraceReqReceive(req, pwc)
	tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "mshr-hit")

	return true
}

func (pwc *PWC) processPWCMSHRMISS( //处理MSHR未命中
	now sim.VTimeInSec,
	req *vm.TranslationReq,
) bool {
	mshrEntry := pwc.mshr.Add(req.PID, req.VAddr) //把查找请求加入mshr
	mshrEntry.Requests = append(mshrEntry.Requests, req)

	pwc.topPort.Retrieve(now)
	tracing.TraceReqReceive(req, pwc)
	tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, pwc), pwc, "mshr-miss")

	pwq := pwqueue.Newpwqueueentry(req, 0) //把查找请求加入pwcache
	err := pwc.pwqueue.Enqueue(pwq)
	if err != nil {
		return false
	}

	return true
}
func (pwc *PWC) fetchBottom(now sim.VTimeInSec, req *vm.TranslationReq, hitlevel int) bool { //从bottom端口发送翻译请求

	Req := vm.TranslationReqBuilder{}.
		WithSendTime(now).
		WithSrc(pwc.bottomPort).
		WithDst(pwc.LowModule).
		WithPID(req.PID).
		WithVAddr(req.VAddr).
		WithDeviceID(req.DeviceID).
		Build()

	fetchBottom := TranslationReqBuilder{}.
		WithSendTime(now).
		WithSrc(pwc.bottomPort).
		WithDst(pwc.LowModule).
		WithPID(req.PID).
		WithVAddr(req.VAddr).
		WithDeviceID(req.DeviceID).
		WithLantency(100 * (4 - hitlevel)).
		WithReq(Req).
		Build()
	err := pwc.bottomPort.Send(fetchBottom)
	if err != nil {
		return false
	}

	mshrEntry := pwc.mshr.Query(req.PID, req.VAddr)
	mshrEntry.reqToBottom = fetchBottom

	tracing.TraceReqInitiate(fetchBottom, pwc,
		tracing.MsgIDAtReceiver(req, pwc))

	return true
}

func (pwc *PWC) parseBottom(now sim.VTimeInSec) bool { //解析bottom传回的rsp
	if pwc.respondingMSHREntry != nil {
		return false
	}

	item := pwc.bottomPort.Peek()
	if item == nil {
		return false
	}

	rsp := item.(*vm.TranslationRsp)
	page := rsp.Page

	mshrEntryPresent := pwc.mshr.IsEntryPresent(rsp.Page.PID, rsp.Page.VAddr)
	if !mshrEntryPresent {
		pwc.bottomPort.Retrieve(now)
		return true
	}
	l4index := page.VAddr >> (pwc.log2PageSize + 27) << (pwc.log2PageSize + 27)
	l3index := page.VAddr >> (pwc.log2PageSize + 18) << (pwc.log2PageSize + 18)
	l2index := page.VAddr >> (pwc.log2PageSize + 9) << (pwc.log2PageSize + 9)

	setID := pwc.vAddrToSetID(l4index)
	set := pwc.Sets[setID]
	wayID, ok := pwc.Sets[setID].Evict()
	if !ok {
		panic("failed to evict")
	}
	set.Update(wayID, page) //把l4index保存在PWC中
	set.Visit(wayID)

	setID = pwc.vAddrToSetID(l3index)
	set = pwc.Sets[setID]
	wayID, ok = pwc.Sets[setID].Evict()
	if !ok {
		panic("failed to evict")
	}
	set.Update(wayID, page) //把l3index页表项保存在PWC中
	set.Visit(wayID)

	setID = pwc.vAddrToSetID(l2index)
	set = pwc.Sets[setID]
	wayID, ok = pwc.Sets[setID].Evict()
	if !ok {
		panic("failed to evict")
	}
	set.Update(wayID, page) //把l2index保存在PWC中
	set.Visit(wayID)

	mshrEntry := pwc.mshr.GetEntry(rsp.Page.PID, rsp.Page.VAddr)
	pwc.respondingMSHREntry = mshrEntry
	mshrEntry.page = page

	pwc.mshr.Remove(rsp.Page.PID, rsp.Page.VAddr)    //从mshr中移除
	pwc.pwqueue.Remove(rsp.Page.PID, rsp.Page.VAddr) //从pwqueue中移除
	pwc.bottomPort.Retrieve(now)
	tracing.TraceReqFinalize(mshrEntry.reqToBottom, pwc)

	return true
}

func (pwc *PWC) performCtrlReq(now sim.VTimeInSec) bool { //处理控制请求
	item := pwc.controlPort.Peek()
	if item == nil {
		return false
	}

	item = pwc.controlPort.Retrieve(now)

	switch req := item.(type) {
	case *FlushReq:
		return pwc.handlePWCFlush(now, req)
	case *RestartReq:
		return pwc.handlePWCRestart(now, req)
	default:
		log.Panicf("cannot process request %s", reflect.TypeOf(req))
	}

	return true
}

func (pwc *PWC) visit(setID, wayID int) {
	set := pwc.Sets[setID]
	set.Visit(wayID)
}

func (pwc *PWC) handlePWCFlush(now sim.VTimeInSec, req *FlushReq) bool {
	rsp := FlushRspBuilder{}.
		WithSrc(pwc.controlPort).
		WithDst(req.Src).
		WithSendTime(now).
		Build()

	err := pwc.controlPort.Send(rsp)
	if err != nil {
		return false
	}

	for _, vAddr := range req.VAddr {
		setID := pwc.vAddrToSetID(vAddr)
		set := pwc.Sets[setID]
		wayID, page, found := set.Lookup(req.PID, vAddr)
		if !found {
			continue
		}

		page.Valid = false
		set.Update(wayID, page)
	}

	pwc.mshr.Reset()
	pwc.isPaused = true
	return true
}

func (pwc *PWC) handlePWCRestart(now sim.VTimeInSec, req *RestartReq) bool {
	rsp := RestartRspBuilder{}.
		WithSendTime(now).
		WithSrc(pwc.controlPort).
		WithDst(req.Src).
		Build()

	err := pwc.controlPort.Send(rsp)
	if err != nil {
		return false
	}

	pwc.isPaused = false

	for pwc.topPort.Retrieve(now) != nil {
		pwc.topPort.Retrieve(now)
	}

	for pwc.bottomPort.Retrieve(now) != nil {
		pwc.bottomPort.Retrieve(now)
	}

	return true
}
