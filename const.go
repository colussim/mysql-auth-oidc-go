// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2012 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import "runtime"

const (
	debug = false // for debugging. Set true only in development.

	defaultAuthPlugin       = "mysql_native_password"
	defaultMaxAllowedPacket = 64 << 20 // 64 MiB. See https://github.com/go-sql-driver/mysql/issues/1355
	minProtocolVersion      = 10
	maxPacketSize           = 1<<24 - 1
	timeFormat              = "2006-01-02 15:04:05.999999"

	// Connection attributes
	// See https://dev.mysql.com/doc/refman/8.0/en/performance-schema-connection-attribute-tables.html#performance-schema-connection-attributes-available
	connAttrClientName      = "_client_name"
	connAttrClientNameValue = "Go-MySQL-Driver"
	connAttrOS              = "_os"
	connAttrOSValue         = runtime.GOOS
	connAttrPlatform        = "_platform"
	connAttrPlatformValue   = runtime.GOARCH
	connAttrPid             = "_pid"
	connAttrServerHost      = "_server_host"
)

// MySQL constants documentation:
// http://dev.mysql.com/doc/internals/en/client-server-protocol.html

const (
	iOK           byte = 0x00
	iAuthMoreData byte = 0x01
	iLocalInFile  byte = 0xfb
	iEOF          byte = 0xfe
	iERR          byte = 0xff
)

// https://dev.mysql.com/doc/dev/mysql-server/latest/group__group__cs__capabilities__flags.html
// https://mariadb.com/kb/en/connection/#capabilities
type capabilityFlag uint32

const (
	clientMySQL capabilityFlag = 1 << iota
	clientFoundRows
	clientLongFlag
	clientConnectWithDB
	clientNoSchema
	clientCompress
	clientODBC
	clientLocalFiles
	clientIgnoreSpace
	clientProtocol41
	clientInteractive
	clientSSL
	clientIgnoreSIGPIPE
	clientTransactions
	clientReserved
	clientSecureConn
	clientMultiStatements
	clientMultiResults
	clientPSMultiResults
	clientPluginAuth
	clientConnectAttrs
	clientPluginAuthLenEncClientData
	clientCanHandleExpiredPasswords
	clientSessionTrack
	clientDeprecateEOF
)

// https://mariadb.com/kb/en/connection/#capabilities
type extendedCapabilityFlag uint32

const (
	progressIndicator extendedCapabilityFlag = 1 << iota
	clientComMulti
	clientStmtBulkOperations
	clientExtendedMetadata
	clientCacheMetadata
	clientUnitBulkResult
)

const (
	comQuit byte = iota + 1
	comInitDB
	comQuery
	comFieldList
	comCreateDB
	comDropDB
	comRefresh
	comShutdown
	comStatistics
	comProcessInfo
	comConnect
	comProcessKill
	comDebug
	comPing
	comTime
	comDelayedInsert
	comChangeUser
	comBinlogDump
	comTableDump
	comConnectOut
	comRegisterSlave
	comStmtPrepare
	comStmtExecute
	comStmtSendLongData
	comStmtClose
	comStmtReset
	comSetOption
	comStmtFetch
)

// https://dev.mysql.com/doc/internals/en/com-query-response.html#packet-Protocol::ColumnType
type fieldType byte

const (
	fieldTypeDecimal fieldType = iota
	fieldTypeTiny
	fieldTypeShort
	fieldTypeLong
	fieldTypeFloat
	fieldTypeDouble
	fieldTypeNULL
	fieldTypeTimestamp
	fieldTypeLongLong
	fieldTypeInt24
	fieldTypeDate
	fieldTypeTime
	fieldTypeDateTime
	fieldTypeYear
	fieldTypeNewDate
	fieldTypeVarChar
	fieldTypeBit
)
const (
	fieldTypeVector fieldType = iota + 0xf2
	fieldTypeInvalid
	fieldTypeBool
	fieldTypeJSON
	fieldTypeNewDecimal
	fieldTypeEnum
	fieldTypeSet
	fieldTypeTinyBLOB
	fieldTypeMediumBLOB
	fieldTypeLongBLOB
	fieldTypeBLOB
	fieldTypeVarString
	fieldTypeString
	fieldTypeGeometry
)

type fieldFlag uint16

const (
	flagNotNULL fieldFlag = 1 << iota
	flagPriKey
	flagUniqueKey
	flagMultipleKey
	flagBLOB
	flagUnsigned
	flagZeroFill
	flagBinary
	flagEnum
	flagAutoIncrement
	flagTimestamp
	flagSet
	flagUnknown1
	flagUnknown2
	flagUnknown3
	flagUnknown4
)

// http://dev.mysql.com/doc/internals/en/status-flags.html
type statusFlag uint16

const (
	statusInTrans statusFlag = 1 << iota
	statusInAutocommit
	statusReserved // Not in documentation
	statusMoreResultsExists
	statusNoGoodIndexUsed
	statusNoIndexUsed
	statusCursorExists
	statusLastRowSent
	statusDbDropped
	statusNoBackslashEscapes
	statusMetadataChanged
	statusQueryWasSlow
	statusPsOutParams
	statusInTransReadonly
	statusSessionStateChanged
)

const (
	cachingSha2PasswordRequestPublicKey          = 2
	cachingSha2PasswordFastAuthSuccess           = 3
	cachingSha2PasswordPerformFullAuthentication = 4
)
