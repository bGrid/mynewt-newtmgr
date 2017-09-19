/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package nmble

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/runtimeco/go-coap"

	"mynewt.apache.org/newt/util"
	. "mynewt.apache.org/newtmgr/nmxact/bledefs"
	"mynewt.apache.org/newtmgr/nmxact/mgmt"
	"mynewt.apache.org/newtmgr/nmxact/nmp"
	"mynewt.apache.org/newtmgr/nmxact/nmxutil"
	"mynewt.apache.org/newtmgr/nmxact/sesn"
	"mynewt.apache.org/newtmgr/nmxact/task"
)

// Implements a BLE session that does not acquire the master resource on
// connect.  The user of this type must acquire the resource manually.
type NakedSesn struct {
	cfg      sesn.SesnCfg
	bx       *BleXport
	conn     *Conn
	mgmtChrs BleMgmtChrs
	txvr     *mgmt.Transceiver
	tq       task.TaskQueue

	wg sync.WaitGroup

	stopChan chan struct{}

	// Protects `enabled` and `opening`.
	mtx sync.Mutex

	// True if the session is open or being opened.
	enabled bool

	// True if session is being opened; used to prevent a full shutdown in
	// mid-open to allow retries.
	opening bool

	shuttingDown bool
}

func (s *NakedSesn) init() error {
	s.conn = NewConn(s.bx)
	s.stopChan = make(chan struct{})

	if s.txvr != nil {
		s.txvr.Stop()
	}

	txvr, err := mgmt.NewTransceiver(true, s.cfg.MgmtProto, 3)
	if err != nil {
		return err
	}
	s.txvr = txvr

	s.tq.Stop(fmt.Errorf("Ensuring task is stopped"))
	if err := s.tq.Start(10); err != nil {
		nmxutil.Assert(false)
		return err
	}

	return nil
}

func NewNakedSesn(bx *BleXport, cfg sesn.SesnCfg) (*NakedSesn, error) {
	mgmtChrs, err := BuildMgmtChrs(cfg.MgmtProto)
	if err != nil {
		return nil, err
	}

	s := &NakedSesn{
		cfg:      cfg,
		bx:       bx,
		mgmtChrs: mgmtChrs,
	}

	if err := s.tq.Start(10); err != nil {
		nmxutil.Assert(false)
		return nil, err
	}

	s.init()

	return s, nil
}

func (s *NakedSesn) shutdown(cause error) error {
	initiate := func() error {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		if s.shuttingDown || !s.enabled {
			return nmxutil.NewSesnClosedError(
				"Attempt to close an already-closed session")
		}
		s.shuttingDown = true

		return nil
	}

	if err := initiate(); err != nil {
		return err
	}
	defer func() {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		s.shuttingDown = false
	}()

	// Stop the task queue to flush all pending events.
	s.tq.StopNoWait(cause)

	s.conn.Stop()

	if s.IsOpen() {
		s.bx.RemoveSesn(s.conn.connHandle)
	}

	// Signal error to all listeners.
	s.txvr.ErrorAll(cause)
	s.txvr.Stop()

	// Stop Goroutines associated with notification listeners.
	close(s.stopChan)

	// Block until close completes.
	s.wg.Wait()

	// Call the on-close callback if the session was fully open.
	s.mtx.Lock()
	opening := s.opening
	s.enabled = false
	s.mtx.Unlock()

	if !opening {
		if s.cfg.OnCloseCb != nil {
			s.cfg.OnCloseCb(s, cause)
		}
	}

	return nil
}

func (s *NakedSesn) enqueueShutdown(cause error) chan error {
	return s.tq.Enqueue(func() error { return s.shutdown(cause) })
}

func (s *NakedSesn) Open() error {
	initiate := func() error {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		if s.opening || s.enabled {
			return nmxutil.NewSesnAlreadyOpenError(
				"Attempt to open an already-open BLE session")
		}

		s.opening = true
		return nil
	}

	if err := initiate(); err != nil {
		return err
	}
	defer func() {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		s.opening = false
	}()

	var err error
	for i := 0; i < s.cfg.Ble.Central.ConnTries; i++ {
		var retry bool

		retry, err = s.openOnce()
		if err != nil {
			s.shutdown(err)
		}

		if !retry {
			break
		}
	}

	if err != nil {
		return err
	}

	s.bx.AddSesn(s.conn.connHandle, s)

	s.mtx.Lock()
	s.enabled = true
	s.mtx.Unlock()

	return nil
}

func (s *NakedSesn) OpenConnected(
	connHandle uint16, eventListener *Listener) error {

	initiate := func() error {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		if s.opening || s.enabled {
			return nmxutil.NewSesnAlreadyOpenError(
				"Attempt to open an already-open BLE session")
		}
		nmxutil.Assert(!s.opening)

		s.opening = true
		return nil
	}

	if err := initiate(); err != nil {
		return err
	}
	defer func() {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		s.opening = false
	}()

	if err := s.init(); err != nil {
		return err
	}

	if err := s.conn.Inherit(connHandle, eventListener); err != nil {
		return err
	}

	// Listen for disconnect in the background.
	s.disconnectListen()

	// Listen for incoming notifications in the background.
	s.notifyListen()

	// Give a record of this open session to the transport.
	s.bx.AddSesn(connHandle, s)

	s.mtx.Lock()
	s.enabled = true
	s.mtx.Unlock()

	return nil
}

func (s *NakedSesn) TxNmpOnce(req *nmp.NmpMsg, opt sesn.TxOptions) (
	nmp.NmpRsp, error) {

	var rsp nmp.NmpRsp

	fn := func() error {
		chr, err := s.getChr(s.mgmtChrs.NmpReqChr)
		if err != nil {
			return err
		}

		txRaw := func(b []byte) error {
			if s.cfg.Ble.WriteRsp {
				return s.conn.WriteChr(chr, b, "nmp")
			} else {
				return s.conn.WriteChrNoRsp(chr, b, "nmp")
			}
		}

		rsp, err = s.txvr.TxNmp(txRaw, req, s.MtuOut(), opt.Timeout)
		return err
	}

	if err := s.tq.Run(fn); err != nil {
		return nil, err
	}

	return rsp, nil
}

func (s *NakedSesn) TxCoapOnce(m coap.Message,
	resType sesn.ResourceType,
	opt sesn.TxOptions) (coap.COAPCode, []byte, error) {

	var rspCode coap.COAPCode
	var rspPayload []byte

	fn := func() error {
		chrId := ResChrReqIdLookup(s.mgmtChrs, resType)
		chr, err := s.getChr(chrId)
		if err != nil {
			return err
		}

		encReqd, authReqd, err := ResTypeSecReqs(resType)
		if err != nil {
			return err
		}
		if err := s.ensureSecurity(encReqd, authReqd); err != nil {
			return err
		}

		txRaw := func(b []byte) error {
			if s.cfg.Ble.WriteRsp {
				return s.conn.WriteChr(chr, b, "coap")
			} else {
				return s.conn.WriteChrNoRsp(chr, b, "coap")
			}
		}

		rsp, err := s.txvr.TxOic(txRaw, m, s.MtuOut(), opt.Timeout)
		if err == nil && rsp != nil {
			rspCode = rsp.Code()
			rspPayload = rsp.Payload()
		}
		return err
	}

	if err := s.tq.Run(fn); err != nil {
		return 0, nil, err
	}

	return rspCode, rspPayload, nil
}

func (s *NakedSesn) AbortRx(seq uint8) error {
	fn := func() error {
		s.txvr.AbortRx(seq)
		return nil
	}
	return s.tq.Run(fn)
}

func (s *NakedSesn) Close() error {
	fn := func() error {
		return s.shutdown(fmt.Errorf("BLE session manually closed"))
	}

	return s.tq.Run(fn)
}

func (s *NakedSesn) IsOpen() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.enabled
}

func (s *NakedSesn) MtuIn() int {
	return int(s.conn.AttMtu()) - NOTIFY_CMD_BASE_SZ
}

func (s *NakedSesn) MtuOut() int {
	return util.IntMin(s.MtuIn(), BLE_ATT_ATTR_MAX_LEN)
}

func (s *NakedSesn) CoapIsTcp() bool {
	return true
}

func (s *NakedSesn) MgmtProto() sesn.MgmtProto {
	return s.cfg.MgmtProto
}

func (s *NakedSesn) ConnInfo() (BleConnDesc, error) {
	return s.conn.ConnInfo(), nil
}

func (s *NakedSesn) openOnce() (bool, error) {
	if err := s.init(); err != nil {
		return false, err
	}

	// Listen for disconnect in the background.
	s.disconnectListen()

	if err := s.conn.Connect(
		s.cfg.Ble.OwnAddrType,
		s.cfg.PeerSpec.Ble,
		s.cfg.Ble.Central.ConnTimeout); err != nil {

		return false, err
	}

	if err := s.conn.ExchangeMtu(); err != nil {
		bhdErr := nmxutil.ToBleHost(err)
		retry := bhdErr != nil && bhdErr.Status == ERR_CODE_ENOTCONN
		return retry, err
	}

	if err := s.conn.DiscoverSvcs(); err != nil {
		return false, err
	}

	if chr, _ := s.getChr(s.mgmtChrs.NmpRspChr); chr != nil {
		if chr.SubscribeType() != 0 {
			if err := s.conn.Subscribe(chr); err != nil {
				return false, err
			}
		}
	}

	if s.cfg.Ble.EncryptWhen == BLE_ENCRYPT_ALWAYS {
		if err := s.conn.InitiateSecurity(); err != nil {
			return false, err
		}
	}

	// Listen for incoming notifications in the background.
	s.notifyListen()

	return false, nil
}

// Listens for disconnect in the background.
func (s *NakedSesn) disconnectListen() {
	discChan := s.conn.DisconnectChan()

	// Terminates on:
	// * Receive from connection disconnect-channel.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Block until disconnect.
		err := <-discChan
		s.enqueueShutdown(err)
	}()
}

func (s *NakedSesn) getChr(chrId *BleChrId) (*Characteristic, error) {
	if chrId == nil {
		return nil, fmt.Errorf("BLE session not configured with required " +
			"characteristic")
	}

	chr := s.conn.Profile().FindChrByUuid(*chrId)
	if chr == nil {
		return nil, fmt.Errorf("BLE peer doesn't support required "+
			"characteristic: %s", chrId.String())
	}

	return chr, nil
}

func (s *NakedSesn) createNotifyListener(chrId *BleChrId) (
	*NotifyListener, error) {

	chr, err := s.getChr(chrId)
	if err != nil {
		return nil, err
	}

	return s.conn.ListenForNotifications(chr)
}

func (s *NakedSesn) notifyListenOnce(chrId *BleChrId,
	dispatchCb func(b []byte)) {

	nl, err := s.createNotifyListener(chrId)
	if err != nil {
		log.Debugf("error listening for notifications: %s", err.Error())
		return
	}

	stopChan := s.stopChan

	// Terminates on:
	// * Notify listener error.
	// * Receive from stop channel.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		for {
			select {
			case <-nl.ErrChan:
				return

			case n, ok := <-nl.NotifyChan:
				if ok {
					dispatchCb(n.Data)
				}

			case <-stopChan:
				return
			}
		}
	}()
}

func (s *NakedSesn) notifyListen() {
	s.notifyListenOnce(s.mgmtChrs.ResUnauthRspChr, s.txvr.DispatchCoap)
	s.notifyListenOnce(s.mgmtChrs.ResSecureRspChr, s.txvr.DispatchCoap)
	s.notifyListenOnce(s.mgmtChrs.ResPublicRspChr, s.txvr.DispatchCoap)
	s.notifyListenOnce(s.mgmtChrs.NmpRspChr, s.txvr.DispatchNmpRsp)
}

func (s *NakedSesn) checkSecurity(encReqd bool, authReqd bool) (bool, bool) {
	desc, _ := s.ConnInfo()

	return !encReqd || desc.Encrypted,
		!authReqd || desc.Authenticated
}

func (s *NakedSesn) ensureSecurity(encReqd bool, authReqd bool) error {
	encGood, authGood := s.checkSecurity(encReqd, authReqd)
	if encGood && authGood {
		return nil
	}

	if err := s.conn.InitiateSecurity(); err != nil {
		return err
	}

	// Ensure pairing meets characteristic's requirements.
	encGood, authGood = s.checkSecurity(encReqd, authReqd)
	if !encGood {
		return fmt.Errorf("Insufficient BLE security; " +
			"characteristic requires encryption")
	}

	if !authGood {
		return fmt.Errorf("Insufficient BLE security; " +
			"characteristic  requires authentication")
	}

	return nil
}
