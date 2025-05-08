// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2016 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

var (
	errConnClosed        = errors.New("connection is closed")
	errConnTooManyReads  = errors.New("too many reads")
	errConnTooManyWrites = errors.New("too many writes")
)

// struct to mock a net.Conn for testing purposes
type mockConn struct {
	laddr         net.Addr
	raddr         net.Addr
	data          []byte
	written       []byte
	queuedReplies [][]byte
	closed        bool
	read          int
	reads         int
	writes        int
	maxReads      int
	maxWrites     int
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.closed {
		return 0, errConnClosed
	}

	m.reads++
	if m.maxReads > 0 && m.reads > m.maxReads {
		return 0, errConnTooManyReads
	}

	n = copy(b, m.data)
	m.read += n
	m.data = m.data[n:]
	return
}
func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.closed {
		return 0, errConnClosed
	}

	m.writes++
	if m.maxWrites > 0 && m.writes > m.maxWrites {
		return 0, errConnTooManyWrites
	}

	n = len(b)
	m.written = append(m.written, b...)

	if n > 0 && len(m.queuedReplies) > 0 {
		m.data = m.queuedReplies[0]
		m.queuedReplies = m.queuedReplies[1:]
	}
	return
}
func (m *mockConn) Close() error {
	m.closed = true
	return nil
}
func (m *mockConn) LocalAddr() net.Addr {
	return m.laddr
}
func (m *mockConn) RemoteAddr() net.Addr {
	return m.raddr
}
func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}
func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// make sure mockConn implements the net.Conn interface
var _ net.Conn = new(mockConn)

func newRWMockConn(sequence uint8) (*mockConn, *mysqlConn) {
	conn := new(mockConn)
	connector := newConnector(NewConfig())
	mc := &mysqlConn{
		buf:              newBuffer(),
		cfg:              connector.cfg,
		connector:        connector,
		netConn:          conn,
		closech:          make(chan struct{}),
		maxAllowedPacket: defaultMaxAllowedPacket,
		sequence:         sequence,
	}
	return conn, mc
}

func TestReadPacketSingleByte(t *testing.T) {
	conn := new(mockConn)
	mc := &mysqlConn{
		netConn: conn,
		buf:     newBuffer(),
		cfg:     NewConfig(),
	}

	conn.data = []byte{0x01, 0x00, 0x00, 0x00, 0xff}
	conn.maxReads = 1
	packet, err := mc.readPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 1 {
		t.Fatalf("unexpected packet length: expected %d, got %d", 1, len(packet))
	}
	if packet[0] != 0xff {
		t.Fatalf("unexpected packet content: expected %x, got %x", 0xff, packet[0])
	}
}

func TestReadPacketWrongSequenceID(t *testing.T) {
	for _, testCase := range []struct {
		ClientSequenceID byte
		ServerSequenceID byte
		ExpectedErr      error
	}{
		{
			ClientSequenceID: 1,
			ServerSequenceID: 0,
			ExpectedErr:      ErrPktSync,
		},
		{
			ClientSequenceID: 0,
			ServerSequenceID: 0x42,
			ExpectedErr:      ErrPktSync,
		},
	} {
		conn, mc := newRWMockConn(testCase.ClientSequenceID)

		conn.data = []byte{0x01, 0x00, 0x00, testCase.ServerSequenceID, 0x22}
		_, err := mc.readPacket()
		if err != testCase.ExpectedErr {
			t.Errorf("expected %v, got %v", testCase.ExpectedErr, err)
		}

		// connection should not be returned to the pool in this state
		if mc.IsValid() {
			t.Errorf("expected IsValid() to be false")
		}
	}
}

func TestReadPacketSplit(t *testing.T) {
	conn := new(mockConn)
	mc := &mysqlConn{
		netConn: conn,
		buf:     newBuffer(),
		cfg:     NewConfig(),
	}

	data := make([]byte, maxPacketSize*2+4*3)
	const pkt2ofs = maxPacketSize + 4
	const pkt3ofs = 2 * (maxPacketSize + 4)

	// case 1: payload has length maxPacketSize
	data = data[:pkt2ofs+4]

	// 1st packet has maxPacketSize length and sequence id 0
	// ff ff ff 00 ...
	data[0] = 0xff
	data[1] = 0xff
	data[2] = 0xff

	// mark the payload start and end of 1st packet so that we can check if the
	// content was correctly appended
	data[4] = 0x11
	data[maxPacketSize+3] = 0x22

	// 2nd packet has payload length 0 and sequence id 1
	// 00 00 00 01
	data[pkt2ofs+3] = 0x01

	conn.data = data
	conn.maxReads = 3
	packet, err := mc.readPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != maxPacketSize {
		t.Fatalf("unexpected packet length: expected %d, got %d", maxPacketSize, len(packet))
	}
	if packet[0] != 0x11 {
		t.Fatalf("unexpected payload start: expected %x, got %x", 0x11, packet[0])
	}
	if packet[maxPacketSize-1] != 0x22 {
		t.Fatalf("unexpected payload end: expected %x, got %x", 0x22, packet[maxPacketSize-1])
	}

	// case 2: payload has length which is a multiple of maxPacketSize
	data = data[:cap(data)]

	// 2nd packet now has maxPacketSize length
	data[pkt2ofs] = 0xff
	data[pkt2ofs+1] = 0xff
	data[pkt2ofs+2] = 0xff

	// mark the payload start and end of the 2nd packet
	data[pkt2ofs+4] = 0x33
	data[pkt2ofs+maxPacketSize+3] = 0x44

	// 3rd packet has payload length 0 and sequence id 2
	// 00 00 00 02
	data[pkt3ofs+3] = 0x02

	conn.data = data
	conn.reads = 0
	conn.maxReads = 5
	mc.sequence = 0
	packet, err = mc.readPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 2*maxPacketSize {
		t.Fatalf("unexpected packet length: expected %d, got %d", 2*maxPacketSize, len(packet))
	}
	if packet[0] != 0x11 {
		t.Fatalf("unexpected payload start: expected %x, got %x", 0x11, packet[0])
	}
	if packet[2*maxPacketSize-1] != 0x44 {
		t.Fatalf("unexpected payload end: expected %x, got %x", 0x44, packet[2*maxPacketSize-1])
	}

	// case 3: payload has a length larger maxPacketSize, which is not an exact
	// multiple of it
	data = data[:pkt2ofs+4+42]
	data[pkt2ofs] = 0x2a
	data[pkt2ofs+1] = 0x00
	data[pkt2ofs+2] = 0x00
	data[pkt2ofs+4+41] = 0x44

	conn.data = data
	conn.reads = 0
	conn.maxReads = 4
	mc.sequence = 0
	packet, err = mc.readPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != maxPacketSize+42 {
		t.Fatalf("unexpected packet length: expected %d, got %d", maxPacketSize+42, len(packet))
	}
	if packet[0] != 0x11 {
		t.Fatalf("unexpected payload start: expected %x, got %x", 0x11, packet[0])
	}
	if packet[maxPacketSize+41] != 0x44 {
		t.Fatalf("unexpected payload end: expected %x, got %x", 0x44, packet[maxPacketSize+41])
	}
}

func TestReadPacketFail(t *testing.T) {
	conn := new(mockConn)
	mc := &mysqlConn{
		netConn: conn,
		buf:     newBuffer(),
		closech: make(chan struct{}),
		cfg:     NewConfig(),
	}

	// illegal empty (stand-alone) packet
	conn.data = []byte{0x00, 0x00, 0x00, 0x00}
	conn.maxReads = 1
	_, err := mc.readPacket()
	if err != ErrInvalidConn {
		t.Errorf("expected ErrInvalidConn, got %v", err)
	}

	// reset
	conn.reads = 0
	mc.sequence = 0
	mc.buf = newBuffer()

	// fail to read header
	conn.closed = true
	_, err = mc.readPacket()
	if err != ErrInvalidConn {
		t.Errorf("expected ErrInvalidConn, got %v", err)
	}

	// reset
	conn.closed = false
	conn.reads = 0
	mc.sequence = 0
	mc.buf = newBuffer()

	// fail to read body
	conn.maxReads = 1
	_, err = mc.readPacket()
	if err != ErrInvalidConn {
		t.Errorf("expected ErrInvalidConn, got %v", err)
	}
}

// https://github.com/go-sql-driver/mysql/pull/801
// not-NUL terminated plugin_name in init packet
func TestRegression801(t *testing.T) {
	conn := new(mockConn)
	mc := &mysqlConn{
		netConn:  conn,
		buf:      newBuffer(),
		cfg:      new(Config),
		sequence: 42,
		closech:  make(chan struct{}),
	}

	conn.data = []byte{72, 0, 0, 42, 10, 53, 46, 53, 46, 56, 0, 165, 0, 0, 0,
		60, 70, 63, 58, 68, 104, 34, 97, 0, 223, 247, 33, 2, 0, 15, 128, 21, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 98, 120, 114, 47, 85, 75, 109, 99, 51, 77,
		50, 64, 0, 109, 121, 115, 113, 108, 95, 110, 97, 116, 105, 118, 101, 95,
		112, 97, 115, 115, 119, 111, 114, 100}
	conn.maxReads = 1

	authData, serverCapabilities, serverExtendedCapabilities, pluginName, err := mc.readHandshakePacket()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if serverCapabilities != 2148530143 {
		t.Fatalf("expected serverCapabilities to be 2148530143, got %v", serverCapabilities)
	}

	if serverExtendedCapabilities != 0 {
		t.Fatalf("expected serverExtendedCapabilities to be 0, got %v", serverExtendedCapabilities)
	}

	if pluginName != "mysql_native_password" {
		t.Errorf("expected plugin name 'mysql_native_password', got '%s'", pluginName)
	}

	expectedAuthData := []byte{60, 70, 63, 58, 68, 104, 34, 97, 98, 120, 114,
		47, 85, 75, 109, 99, 51, 77, 50, 64}
	if !bytes.Equal(authData, expectedAuthData) {
		t.Errorf("expected authData '%v', got '%v'", expectedAuthData, authData)
	}
}
func TestWritePacket_Normal(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	type testMysqlConn struct {
		*mysqlConn
		writeWithTimeoutFunc func(b []byte) (int, error)
	}

	type testMysqlConn struct {
		*mysqlConn
		writeWithTimeoutFunc func(b []byte) (int, error)
	}
	
	func (tmc *testMysqlConn) writeWithTimeout(b []byte) (int, error) {
		if tmc.writeWithTimeoutFunc != nil {
			return tmc.writeWithTimeoutFunc(b)
		}
		return tmc.mysqlConn.writeWithTimeout(b)
	}
	
	tmc := &testMysqlConn{
		mysqlConn: mc,
		writeWithTimeoutFunc: func(b []byte) (int, error) {
			return len(b), nil
		},
	}
	mc = tmc.mysqlConn
	data := make([]byte, 4+10)
	copy(data[4:], []byte("0123456789"))
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mc.sequence != 1 {
		t.Errorf("expected sequence 1, got %d", mc.sequence)
	}
	if !bytes.Equal(conn.written[4:], []byte("0123456789")) {
		t.Errorf("unexpected written data: %v", conn.written)
	}
}

func TestWritePacket_TooLarge(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 8
	data := make([]byte, 4+16)
	err := mc.writePacket(data)
	if err != ErrPktTooLarge {
		t.Errorf("expected ErrPktTooLarge, got %v", err)
	}
}

func TestWritePacket_WriteErrorFirstWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	called := false
	mc.writeWithTimeout = func(b []byte) (int, error) {
		called = true
		return 0, io.ErrUnexpectedEOF
	}
	data := make([]byte, 4+10)
	err := mc.writePacket(data)
	if !called {
		t.Fatal("writeWithTimeout not called")
	}
	if err != errBadConnNoWrite {
		t.Errorf("expected errBadConnNoWrite, got %v", err)
	}
}

func TestWritePacket_WriteErrorAfterPartialWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	called := false
	mc.writeWithTimeout = func(b []byte) (int, error) {
		called = true
		return 5, io.ErrUnexpectedEOF
	}
	data := make([]byte, 4+10)
	err := mc.writePacket(data)
	if !called {
		t.Fatal("writeWithTimeout not called")
	}
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestWritePacket_ShortWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	mc.writeWithTimeout = func(b []byte) (int, error) {
		return len(b) - 1, nil
	}
	data := make([]byte, 4+10)
	err := mc.writePacket(data)
	if err != io.ErrShortWrite {
		t.Errorf("expected io.ErrShortWrite, got %v", err)
	}
}

func TestWritePacket_MultiPacket(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 2 * maxPacketSize
	mc.writeWithTimeout = func(b []byte) (int, error) {
		return len(b), nil
	}
	// Data size: 4 + (maxPacketSize + 10)
	data := make([]byte, 4+maxPacketSize+10)
	for i := range data[4:] {
		data[4+i] = byte(i % 256)
	}
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// Should have written two packets, sequence incremented twice
	if mc.sequence != 2 {
		t.Errorf("expected sequence 2, got %d", mc.sequence)
	}
	// First packet size
	firstPktLen := 4 + maxPacketSize
	if len(conn.written) < firstPktLen+14 {
		t.Errorf("written data too short: %d", len(conn.written))
	}
	// Check first packet header
	if conn.written[0] != 0xFF || conn.written[1] != 0xFF || conn.written[2] != 0xFF || conn.written[3] != 0x00 {
		t.Errorf("unexpected first packet header: %v", conn.written[:4])
	}
	// Check second packet header
	off := firstPktLen
	if conn.written[off] != 10 || conn.written[off+1] != 0x00 || conn.written[off+2] != 0x00 || conn.written[off+3] != 0x01 {
		t.Errorf("unexpected second packet header: %v", conn.written[off:off+4])
	}
}

type fakeCompIO struct {
	writeCalled bool
}

func (f *fakeCompIO) writePackets(b []byte) (int, error) {
	f.writeCalled = true
	return len(b), nil
}

func TestWritePacket_Compress(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	comp := &fakeCompIO{}
	mc.compIO = comp
	mc.compress = true
	data := make([]byte, 4+10)
	copy(data[4:], []byte("abcdefghij"))
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !comp.writeCalled {
		t.Errorf("expected compIO.writePackets to be called")
	}
}
func TestWritePacket_SendsPacketCorrectly(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	mc.writeWithTimeout = func(b []byte) (int, error) {
		return len(b), nil
	}
	data := make([]byte, 4+8)
	copy(data[4:], []byte("testdata"))
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mc.sequence != 1 {
		t.Errorf("expected sequence 1, got %d", mc.sequence)
	}
	if !bytes.Equal(conn.written[4:], []byte("testdata")) {
		t.Errorf("unexpected written data: %v", conn.written)
	}
}

func TestWritePacket_ReturnsErrPktTooLarge(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 4
	data := make([]byte, 4+10)
	err := mc.writePacket(data)
	if err != ErrPktTooLarge {
		t.Errorf("expected ErrPktTooLarge, got %v", err)
	}
}

func TestWritePacket_WriteErrorFirstWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	called := false
	mc.writeWithTimeout = func(b []byte) (int, error) {
		called = true
		return 0, io.ErrUnexpectedEOF
	}
	data := make([]byte, 4+8)
	err := mc.writePacket(data)
	if !called {
		t.Fatal("writeWithTimeout not called")
	}
	if err != errBadConnNoWrite {
		t.Errorf("expected errBadConnNoWrite, got %v", err)
	}
}

func TestWritePacket_WriteErrorAfterPartialWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	called := false
	mc.writeWithTimeout = func(b []byte) (int, error) {
		called = true
		return 5, io.ErrUnexpectedEOF
	}
	data := make([]byte, 4+8)
	err := mc.writePacket(data)
	if !called {
		t.Fatal("writeWithTimeout not called")
	}
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestWritePacket_ShortWrite(t *testing.T) {
	_, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	mc.writeWithTimeout = func(b []byte) (int, error) {
		return len(b) - 1, nil
	}
	data := make([]byte, 4+8)
	err := mc.writePacket(data)
	if err != io.ErrShortWrite {
		t.Errorf("expected io.ErrShortWrite, got %v", err)
	}
}

func TestWritePacket_MultiPacket(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 2 * maxPacketSize
	mc.writeWithTimeout = func(b []byte) (int, error) {
		return len(b), nil
	}
	data := make([]byte, 4+maxPacketSize+5)
	for i := range data[4:] {
		data[4+i] = byte(i % 256)
	}
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mc.sequence != 2 {
		t.Errorf("expected sequence 2, got %d", mc.sequence)
	}
	firstPktLen := 4 + maxPacketSize
	if len(conn.written) < firstPktLen+9 {
		t.Errorf("written data too short: %d", len(conn.written))
	}
	off := firstPktLen
	if conn.written[off] != 5 || conn.written[off+1] != 0x00 || conn.written[off+2] != 0x00 || conn.written[off+3] != 0x01 {
		t.Errorf("unexpected second packet header: %v", conn.written[off:off+4])
	}
}

type fakeCompIO struct {
	writeCalled bool
}

func (f *fakeCompIO) writePackets(b []byte) (int, error) {
	f.writeCalled = true
	return len(b), nil
}

func TestWritePacket_Compress(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.maxAllowedPacket = 1024
	comp := &fakeCompIO{}
	mc.compIO = comp
	mc.compress = true
	data := make([]byte, 4+8)
	copy(data[4:], []byte("compress"))
	err := mc.writePacket(data)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !comp.writeCalled {
		t.Errorf("expected compIO.writePackets to be called")
	}
}
