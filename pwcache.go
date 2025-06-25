package pwcache

import (
	"log"
	"reflect"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/akita/v3/tracing"
)

// A TLB is a cache that maintains some page information.
type TLB struct {
	*sim.TickingComponent

	topPort     sim.Port
	bottomPort  sim.Port
	controlPort sim.Port

	LowModule sim.Port

	numSets        int
	numWays        int
	pageSize       uint64
	numReqPerCycle int

	Sets []Set

	mshr                mshr
	respondingMSHREntry *mshrEntry

	isPaused bool
}

// Reset sets all the entries int he TLB to be invalid
func (tlb *TLB) reset() {
	tlb.Sets = make([]Set, tlb.numSets)
	for i := 0; i < tlb.numSets; i++ {
		set := NewSet(tlb.numWays)
		tlb.Sets[i] = set
	}
}

// Tick defines how TLB update states at each cycle
func (tlb *TLB) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = tlb.performCtrlReq(now) || madeProgress

	if !tlb.isPaused {
		for i := 0; i < tlb.numReqPerCycle; i++ {
			madeProgress = tlb.respondMSHREntry(now) || madeProgress
		}

		for i := 0; i < tlb.numReqPerCycle; i++ {
			madeProgress = tlb.lookup(now) || madeProgress
		}

		for i := 0; i < tlb.numReqPerCycle; i++ {
			madeProgress = tlb.parseBottom(now) || madeProgress
		}
	}

	return madeProgress
}

func (tlb *TLB) respondMSHREntry(now sim.VTimeInSec) bool { //正返回的mshr表项
	if tlb.respondingMSHREntry == nil {
		return false
	}

	mshrEntry := tlb.respondingMSHREntry
	page := mshrEntry.page
	req := mshrEntry.Requests[0]
	rspToTop := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(tlb.topPort).
		WithDst(req.Src).
		WithRspTo(req.ID).
		WithPage(page).
		Build()
	err := tlb.topPort.Send(rspToTop)
	if err != nil {
		return false
	}

	mshrEntry.Requests = mshrEntry.Requests[1:]
	if len(mshrEntry.Requests) == 0 {
		tlb.respondingMSHREntry = nil
	}

	tracing.TraceReqComplete(req, tlb)
	return true
}

func (tlb *TLB) lookup(now sim.VTimeInSec) bool {
	msg := tlb.topPort.Peek()
	if msg == nil {
		return false
	}

	req := msg.(*vm.TranslationReq)

	mshrEntry := tlb.mshr.Query(req.PID, req.VAddr) //在mshr中查找
	if mshrEntry != nil {                           //如果找到了
		return tlb.processTLBMSHRHit(now, mshrEntry, req) //处理mshr命中
	}

	setID := tlb.vAddrToSetID(req.VAddr) //计算setID
	set := tlb.Sets[setID]
	wayID, page, found := set.Lookup(req.PID, req.VAddr) //在set中查找
	if found && page.Valid {                             //如果找到了且有效
		return tlb.handleTranslationHit(now, req, setID, wayID, page) //处理tlb命中
	}

	return tlb.handleTranslationMiss(now, req) //处理tlb未命中
}

func (tlb *TLB) handleTranslationHit( //tlb命中
	now sim.VTimeInSec,
	req *vm.TranslationReq,
	setID, wayID int,
	page vm.Page,
) bool {
	ok := tlb.sendRspToTop(now, req, page) //发送rsp到top
	if !ok {
		return false
	}

	tlb.visit(setID, wayID)
	tlb.topPort.Retrieve(now) //从top端口取走req
	//记录
	tracing.TraceReqReceive(req, tlb)
	tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, tlb), tlb, "hit")
	tracing.TraceReqComplete(req, tlb)

	return true
}

func (tlb *TLB) handleTranslationMiss( //处理tlb未命中
	now sim.VTimeInSec,
	req *vm.TranslationReq,
) bool {
	if tlb.mshr.IsFull() {
		return false
	}

	fetched := tlb.fetchBottom(now, req) //向下层的部件发送翻译请求
	if fetched {
		tlb.topPort.Retrieve(now) //从top端口取走req
		tracing.TraceReqReceive(req, tlb)
		tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, tlb), tlb, "miss")
		return true
	}

	return false
}

func (tlb *TLB) vAddrToSetID(vAddr uint64) (setID int) {
	return int(vAddr / tlb.pageSize % uint64(tlb.numSets))
}

func (tlb *TLB) sendRspToTop(
	now sim.VTimeInSec,
	req *vm.TranslationReq,
	page vm.Page,
) bool {
	rsp := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(tlb.topPort).
		WithDst(req.Src).
		WithRspTo(req.ID).
		WithPage(page).
		Build()

	err := tlb.topPort.Send(rsp) //topport发送rsp

	return err == nil
}

func (tlb *TLB) processTLBMSHRHit( //处理MSHR命中
	now sim.VTimeInSec,
	mshrEntry *mshrEntry,
	req *vm.TranslationReq,
) bool {
	mshrEntry.Requests = append(mshrEntry.Requests, req)

	tlb.topPort.Retrieve(now)
	tracing.TraceReqReceive(req, tlb)
	tracing.AddTaskStep(tracing.MsgIDAtReceiver(req, tlb), tlb, "mshr-hit")

	return true
}

func (tlb *TLB) fetchBottom(now sim.VTimeInSec, req *vm.TranslationReq) bool { //从bottom端口发送翻译请求
	fetchBottom := vm.TranslationReqBuilder{}.
		WithSendTime(now).
		WithSrc(tlb.bottomPort).
		WithDst(tlb.LowModule).
		WithPID(req.PID).
		WithVAddr(req.VAddr).
		WithDeviceID(req.DeviceID).
		Build()
	err := tlb.bottomPort.Send(fetchBottom)
	if err != nil {
		return false
	}

	mshrEntry := tlb.mshr.Add(req.PID, req.VAddr) //把查找请求加入mshr
	mshrEntry.Requests = append(mshrEntry.Requests, req)
	mshrEntry.reqToBottom = fetchBottom

	tracing.TraceReqInitiate(fetchBottom, tlb,
		tracing.MsgIDAtReceiver(req, tlb))

	return true
}

func (tlb *TLB) parseBottom(now sim.VTimeInSec) bool { //解析bottom传回的rsp
	if tlb.respondingMSHREntry != nil {
		return false
	}

	item := tlb.bottomPort.Peek()
	if item == nil {
		return false
	}

	rsp := item.(*vm.TranslationRsp)
	page := rsp.Page

	mshrEntryPresent := tlb.mshr.IsEntryPresent(rsp.Page.PID, rsp.Page.VAddr)
	if !mshrEntryPresent {
		tlb.bottomPort.Retrieve(now)
		return true
	}

	setID := tlb.vAddrToSetID(page.VAddr)
	set := tlb.Sets[setID]
	wayID, ok := tlb.Sets[setID].Evict()
	if !ok {
		panic("failed to evict")
	}
	set.Update(wayID, page) //把页表项保存在TLB中
	set.Visit(wayID)

	mshrEntry := tlb.mshr.GetEntry(rsp.Page.PID, rsp.Page.VAddr)
	tlb.respondingMSHREntry = mshrEntry
	mshrEntry.page = page

	tlb.mshr.Remove(rsp.Page.PID, rsp.Page.VAddr)
	tlb.bottomPort.Retrieve(now)
	tracing.TraceReqFinalize(mshrEntry.reqToBottom, tlb)

	return true
}

func (tlb *TLB) performCtrlReq(now sim.VTimeInSec) bool { //处理控制请求
	item := tlb.controlPort.Peek()
	if item == nil {
		return false
	}

	item = tlb.controlPort.Retrieve(now)

	switch req := item.(type) {
	case *FlushReq:
		return tlb.handleTLBFlush(now, req)
	case *RestartReq:
		return tlb.handleTLBRestart(now, req)
	default:
		log.Panicf("cannot process request %s", reflect.TypeOf(req))
	}

	return true
}

func (tlb *TLB) visit(setID, wayID int) {
	set := tlb.Sets[setID]
	set.Visit(wayID)
}

func (tlb *TLB) handleTLBFlush(now sim.VTimeInSec, req *FlushReq) bool {
	rsp := FlushRspBuilder{}.
		WithSrc(tlb.controlPort).
		WithDst(req.Src).
		WithSendTime(now).
		Build()

	err := tlb.controlPort.Send(rsp)
	if err != nil {
		return false
	}

	for _, vAddr := range req.VAddr {
		setID := tlb.vAddrToSetID(vAddr)
		set := tlb.Sets[setID]
		wayID, page, found := set.Lookup(req.PID, vAddr)
		if !found {
			continue
		}

		page.Valid = false
		set.Update(wayID, page)
	}

	tlb.mshr.Reset()
	tlb.isPaused = true
	return true
}

func (tlb *TLB) handleTLBRestart(now sim.VTimeInSec, req *RestartReq) bool {
	rsp := RestartRspBuilder{}.
		WithSendTime(now).
		WithSrc(tlb.controlPort).
		WithDst(req.Src).
		Build()

	err := tlb.controlPort.Send(rsp)
	if err != nil {
		return false
	}

	tlb.isPaused = false

	for tlb.topPort.Retrieve(now) != nil {
		tlb.topPort.Retrieve(now)
	}

	for tlb.bottomPort.Retrieve(now) != nil {
		tlb.bottomPort.Retrieve(now)
	}

	return true
}
