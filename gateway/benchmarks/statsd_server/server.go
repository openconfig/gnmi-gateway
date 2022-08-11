package statsd_server

import (
	"fmt"
	"bytes"
	"errors"
	"io"
	"math"
	"net"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)


var (
	timestampRe, _ = regexp.Compile("[tT]imestamp.?.?: ?\"?([0-9]+)")
)

const (
	tcpListenBufferSize = 1024 * 1024
)

// A simple statsd server used for benchmark purposes
type StatsdSever struct {
	address string
	port int
	// If set, the server will stop after receiving the specified
	// amount of notifications.
	expNotifications int

	done chan struct{}
	stopMutex sync.Mutex
	stopped bool

	wg sync.WaitGroup

	latencyMutex sync.RWMutex
	notifLatencyMs []uint32

	receivedTcpNotif uint64
	receivedUdpNotif uint64

	log zerolog.Logger

	udpListener *net.UDPConn
	tcpListener net.Listener
}

func NewServer(
	address string,
	port int,
	expNotifications int,
	log zerolog.Logger,
) (*StatsdSever, error) {
	server := &StatsdSever{
		address: address,
		port: port,
		expNotifications: expNotifications,
		notifLatencyMs: make([]uint32, 0, expNotifications),
		log: log,
		done: make(chan struct{}),
	}

	return server, nil
}

func (s *StatsdSever) Start() error {
	s.log.Info().Msg("starting server")
	go s.listenUDP()
	go s.listenTCP()

	// TODO: consider waiting for an event
	time.Sleep(1 * time.Second)

	s.log.Info().Msg("started server")
	return nil
}

func (s *StatsdSever) Stop() {
	s.log.Info().Msg("stopping server")

	s.stopMutex.Lock()
	defer s.stopMutex.Unlock()

	if !s.stopped {
		close(s.done)

		if s.tcpListener != nil {
			s.tcpListener.Close()
		}

		if s.udpListener != nil {
			s.udpListener.Close()
		}

		s.stopped = true
	}

	s.log.Info().Msg("stopped server")
}

func (s *StatsdSever) Wait() {
	s.wg.Wait()
}

func (s *StatsdSever) appendLatency(val uint32) {
	s.latencyMutex.Lock()
	defer s.latencyMutex.Unlock()

	s.notifLatencyMs = append(s.notifLatencyMs, val)
}

func (s *StatsdSever) ResetStats() {
	defer s.latencyMutex.Unlock()
	s.latencyMutex.Lock()

	s.notifLatencyMs = make([]uint32, 0, s.expNotifications)
	s.receivedTcpNotif = 0
	s.receivedUdpNotif = 0
}

func (s *StatsdSever) PrintStats() {
	defer s.latencyMutex.RUnlock()
	s.latencyMutex.RLock()

	sortedLatency := s.notifLatencyMs
	sort.Slice(sortedLatency, func(i, j int) bool { return sortedLatency[i] < sortedLatency[j] })

	var mean float64
	var median uint32
	var min90 uint32
	var max90 uint32
	var stdDev float64
	var variance float64
	var min uint32
	var max uint32

	var sum uint64
	for _, val := range sortedLatency {
		sum += uint64(val)
	}
	if len(sortedLatency) > 0 {
		min = sortedLatency[0]
		max = sortedLatency[len(sortedLatency) - 1]
		mean = float64(sum / uint64(len(sortedLatency)))
		median = sortedLatency[len(sortedLatency) / 2]
		max90 = sortedLatency[int(0.9 * float32(len(sortedLatency)))]
		min90 = sortedLatency[int(0.1 * float32(len(sortedLatency)))]

		for _, val := range sortedLatency {
			variance += math.Pow(float64(val) - mean, 2)
		}

		variance /= float64(len(sortedLatency))
		stdDev = math.Sqrt(float64(variance))
	}

	fmt.Printf("\nReceived tcp notifications: %d\n", s.receivedTcpNotif)
	fmt.Printf("Received udp notifications: %d\n", s.receivedUdpNotif)

	fmt.Printf("\nLatency (ms) statistics\n")
	fmt.Printf("min : %d\n", min)
	fmt.Printf("max : %d\n", max)
	fmt.Printf("sum : %d\n", sum)
	fmt.Printf("median : %d\n", median)
	fmt.Printf("mean : %f\n", mean)
	fmt.Printf("variance : %f\n", variance)
	fmt.Printf("stdDev : %f\n", stdDev)
	fmt.Printf("count : %d\n", len(sortedLatency))
	fmt.Printf("min 90 : %d\n", min90)
	fmt.Printf("max 90 : %d\n", max90)
}

func (s *StatsdSever) listenUDP() error {
	s.wg.Add(1)
	defer s.wg.Done()

	srv, err := net.ListenUDP(
		"udp",
		&net.UDPAddr{
			IP: net.ParseIP(s.address),
			Port: s.port,
			Zone: "",
		})
	if err != nil {
		s.log.Err(err).Msg("couldn't setup udp listener")
		return err
	}
	defer srv.Close()
	s.udpListener = srv

	s.log.Info().Msg("started udp listener")

	buf := make([]byte, 1024 * 64)
	for {
		select {
		case <- s.done:
			s.log.Info().Msg("shutting down udp listener")
			return nil
		default:
			n, err := srv.Read(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					s.log.Info().Msg("udp listener closed")
				} else {
					s.log.Err(err).Msg("udp read error")
				}
				continue
			}
			if n == 0 {
				s.log.Info().Msg("empty udp message")
				continue
			}
			bufStr := string(buf[:n])
			// Received single notification
			if strings.Index(bufStr, "\n") == -1 {
				s.handleNotification(bufStr, &s.receivedUdpNotif)
			} else {
				// Received a batch of notifications
				for _, notif := range strings.Split(bufStr, "\n") {
					s.handleNotification(notif, &s.receivedUdpNotif)
				}
			}
			if s.expNotifications > 0 && s.receivedUdpNotif >= uint64(s.expNotifications) {
				s.log.Info().Msg("received all notifications")
				s.Stop()
				return nil
			}
		}
	}

	return nil
}

func (s *StatsdSever) handleNotification(notif string, counter *uint64) {
	if notif == "" {
		return
	}

	atomic.AddUint64(counter, 1)

	matches := timestampRe.FindStringSubmatch(notif)
	if len(matches) >= 2 && matches[1] != "" {
		timestampStr := matches[1]
		timestampEpochNs, err := strconv.ParseInt(timestampStr, 10, 64)
		if err == nil {
		    timestamp := time.Unix(0, timestampEpochNs)
			latency := time.Now().Sub(timestamp)
			s.appendLatency(uint32(latency.Milliseconds()))
		} else {
			s.log.Err(err).Msgf("couldn't parse timestamp: %s, %s", timestampStr, err)
		}
	} else {
		s.log.Info().Msgf("missing notification timestamp: %s %d", notif, len(notif))
	}
}

func (s *StatsdSever) listenTCP() error {
	s.wg.Add(1)
	defer s.wg.Done()

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.address, s.port))
	if err != nil {
		s.log.Err(err).Msg("couldn't setup tcp listener")
		return err
	}
	s.log.Info().Msg("started tcp listener")
	defer ln.Close()
	s.tcpListener = ln

	for {
		select {
		case <- s.done:
			s.log.Info().Msg("tcp listener shutting down")
			return nil
		default:
			s.log.Info().Msg("waiting for tcp connections")
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					s.log.Info().Msg("tcp connection closed")
				} else {
					s.log.Err(err).Msg("tcp connection failure")
				}
				continue
			}

			go func(c net.Conn) {
				defer c.Close()

				buf := bytes.NewBuffer(make([]byte, 0, tcpListenBufferSize))
				for {
					select {
					case <- s.done:
						s.log.Info().Msg("shutting down tcp listener")
						return
					default:
						written, err := io.CopyN(buf, c, 1)
						if err == io.EOF {
							// connection closed
							break
						} else if err != nil {
							s.log.Err(err).Msg("tcp read error")
							break
						}
						if written == 0 {
							continue
						}

						if bytes.Index(buf.Bytes(), []byte("\n")) != - 1 {
							notif, err := buf.ReadString('\n')
							if notif != "" {
								s.handleNotification(notif, &s.receivedTcpNotif)
								if s.expNotifications > 0 && s.receivedTcpNotif >= uint64(s.expNotifications) {
									s.log.Info().Msg("received all notifications")
									s.Stop()
									return
								}
							}
							if err == io.EOF {
								continue
							}
							if err != nil {
								s.log.Err(err).Msg("couldn't parse notification")
								break
							}
						}

						if buf.Len() > int(tcpListenBufferSize) {
							newBuf := bytes.NewBuffer(make([]byte, 0, tcpListenBufferSize))
							_, err := io.Copy(newBuf, buf)
							if err != nil {
								s.log.Err(err).Msg("couldn't rotate buffer")
								break
							}
							buf = newBuf
							s.log.Info().Msg("rotated tcp receive buffer")
						}
					}
				}
			}(conn)
		}
	}

	return nil
}
