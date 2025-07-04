// Package vm provides the models for address translations
package pwcache

import (
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
)

// A TranslationReq asks the receiver component to translate the request.
type TranslationReqpwc struct {
	sim.MsgMeta
	VAddr    uint64
	PID      vm.PID
	DeviceID uint64
	Lantency int
	Req      *vm.TranslationReq
}

// Meta returns the meta data associated with the message.
func (r *TranslationReqpwc) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

// TranslationReqBuilder can build translation requests
type TranslationReqBuilder struct {
	sendTime sim.VTimeInSec
	src, dst sim.Port
	vAddr    uint64
	pid      vm.PID
	deviceID uint64
	lantency int
	req      *vm.TranslationReq
}

// WithSendTime sets the send time of the request to build.:w
func (b TranslationReqBuilder) WithSendTime(
	t sim.VTimeInSec,
) TranslationReqBuilder {
	b.sendTime = t
	return b
}

// WithSrc sets the source of the request to build.
func (b TranslationReqBuilder) WithSrc(src sim.Port) TranslationReqBuilder {
	b.src = src
	return b
}

// WithDst sets the destination of the request to build.
func (b TranslationReqBuilder) WithDst(dst sim.Port) TranslationReqBuilder {
	b.dst = dst
	return b
}

// WithVAddr sets the virtual address of the request to build.
func (b TranslationReqBuilder) WithVAddr(vAddr uint64) TranslationReqBuilder {
	b.vAddr = vAddr
	return b
}

// WithPID sets the virtual address of the request to build.
func (b TranslationReqBuilder) WithPID(pid vm.PID) TranslationReqBuilder {
	b.pid = pid
	return b
}

// WithDeviceID sets the GPU ID of the request to build.
func (b TranslationReqBuilder) WithDeviceID(deviceID uint64) TranslationReqBuilder {
	b.deviceID = deviceID
	return b
}

// WithLantency sets the latency of the request to build.
func (b TranslationReqBuilder) WithLantency(lantency int) TranslationReqBuilder {
	b.lantency = lantency
	return b
}

// WithReq sets the request of the request to build.
func (b TranslationReqBuilder) WithReq(req *vm.TranslationReq) TranslationReqBuilder {
	b.req = req
	return b
}

// Build creates a new TranslationReq
func (b TranslationReqBuilder) Build() *TranslationReqpwc {
	r := &TranslationReqpwc{}
	r.ID = sim.GetIDGenerator().Generate()
	r.Src = b.src
	r.Dst = b.dst
	r.SendTime = b.sendTime
	r.VAddr = b.vAddr
	r.PID = b.pid
	r.DeviceID = b.deviceID
	r.Lantency = b.lantency
	r.Req = b.req
	return r
}

// A TranslationRsp is the respond for a TranslationReq. It carries the physical
// address.
type TranslationRsp struct {
	sim.MsgMeta
	RespondTo string // The ID of the request it replies
	Page      vm.Page
}

// Meta returns the meta data associated with the message.
func (r *TranslationRsp) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

// GetRspTo returns the request ID that the respond is responding to.
func (r *TranslationRsp) GetRspTo() string {
	return r.RespondTo
}

// TranslationRspBuilder can build translation requests
type TranslationRspBuilder struct {
	sendTime sim.VTimeInSec
	src, dst sim.Port
	rspTo    string
	page     vm.Page
}

// WithSendTime sets the send time of the message to build.
func (b TranslationRspBuilder) WithSendTime(
	t sim.VTimeInSec,
) TranslationRspBuilder {
	b.sendTime = t
	return b
}

// WithSrc sets the source of the respond to build.
func (b TranslationRspBuilder) WithSrc(src sim.Port) TranslationRspBuilder {
	b.src = src
	return b
}

// WithDst sets the destination of the respond to build.
func (b TranslationRspBuilder) WithDst(dst sim.Port) TranslationRspBuilder {
	b.dst = dst
	return b
}

// WithRspTo sets the request ID of the respond to build.
func (b TranslationRspBuilder) WithRspTo(rspTo string) TranslationRspBuilder {
	b.rspTo = rspTo
	return b
}

// WithPage sets the page of the respond to build.
func (b TranslationRspBuilder) WithPage(page vm.Page) TranslationRspBuilder {
	b.page = page
	return b
}

// Build creates a new TranslationRsp
func (b TranslationRspBuilder) Build() *TranslationRsp {
	r := &TranslationRsp{}
	r.ID = sim.GetIDGenerator().Generate()
	r.Src = b.src
	r.Dst = b.dst
	r.SendTime = b.sendTime
	r.RespondTo = b.rspTo
	r.Page = b.page
	return r
}

type PageMigrationInfo struct {
	GPUReqToVAddrMap map[uint64][]uint64
}

// PageMigrationReqToDriver is a req to driver from MMU to start page migration process
type PageMigrationReqToDriver struct {
	sim.MsgMeta

	StartTime         sim.VTimeInSec
	EndTime           sim.VTimeInSec
	MigrationInfo     *PageMigrationInfo
	CurrAccessingGPUs []uint64
	PID               vm.PID
	CurrPageHostGPU   uint64
	PageSize          uint64
	RespondToTop      bool
}

// Meta returns the meta data associated with the message.
func (m *PageMigrationReqToDriver) Meta() *sim.MsgMeta {
	return &m.MsgMeta
}

// NewPageMigrationReqToDriver creates a PageMigrationReqToDriver.
func NewPageMigrationReqToDriver(
	time sim.VTimeInSec,
	src, dst sim.Port,
) *PageMigrationReqToDriver {
	cmd := new(PageMigrationReqToDriver)
	cmd.SendTime = time
	cmd.Src = src
	cmd.Dst = dst
	return cmd
}

// PageMigrationRspFromDriver is a rsp from driver to MMU marking completion of migration
type PageMigrationRspFromDriver struct {
	sim.MsgMeta

	StartTime sim.VTimeInSec
	EndTime   sim.VTimeInSec
	VAddr     []uint64
	RspToTop  bool
}

// Meta returns the meta data associated with the message.
func (m *PageMigrationRspFromDriver) Meta() *sim.MsgMeta {
	return &m.MsgMeta
}

// NewPageMigrationRspFromDriver creates a new PageMigrationRspFromDriver.
func NewPageMigrationRspFromDriver(
	time sim.VTimeInSec,
	src, dst sim.Port,
) *PageMigrationRspFromDriver {
	cmd := new(PageMigrationRspFromDriver)
	cmd.SendTime = time
	cmd.Src = src
	cmd.Dst = dst
	return cmd
}
