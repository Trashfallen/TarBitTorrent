package main

// DHT. Серверы создает программа, а не движок, по двум причинам:
//   - движок anacrolix не смотрит на флаг private: обертка не дает объявлять
//     приватные раздачи в DHT;
//   - таблица узлов сохраняется в dht4.nodes/dht6.nodes и при следующем
//     запуске используется вместе с узлами-маршрутизаторами: поиск идет сразу.

import (
	"errors"
	"net"
	"strings"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
)

var errPrivateDHT = errors.New("приватная раздача: в DHT не объявляется")

type dhtNode struct {
	s       *dht.Server
	network string // udp4 | udp6
}

type privateFilterDHT struct {
	torrent.AnacrolixDhtServerWrapper
	isPrivate func(ih [20]byte) bool
}

func (p privateFilterDHT) Announce(hash [20]byte, port int, impliedPort bool) (torrent.DhtAnnounce, error) {
	if p.isPrivate(hash) {
		return nil, errPrivateDHT
	}
	return p.AnacrolixDhtServerWrapper.Announce(hash, port, impliedPort)
}

func (e *Engine) isPrivate(ih [20]byte) bool {
	id := hexHash(ih)
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	return ok && it.Private
}

func hexHash(h [20]byte) string {
	const digits = "0123456789abcdef"
	b := make([]byte, 40)
	for i, c := range h {
		b[i*2], b[i*2+1] = digits[c>>4], digits[c&15]
	}
	return string(b)
}

func (e *Engine) startDHT(cl *torrent.Client) {
	for _, l := range cl.Listeners() {
		pc, ok := l.(net.PacketConn)
		if !ok {
			continue
		}
		s, err := cl.NewAnacrolixDhtServer(pc)
		if err != nil {
			logf("DHT не запустился на %s: %v", pc.LocalAddr(), err)
			continue
		}
		cl.AddDhtServer(privateFilterDHT{torrent.AnacrolixDhtServerWrapper{Server: s}, e.isPrivate})
		network := "udp4"
		if ua, ok := pc.LocalAddr().(*net.UDPAddr); ok && ua.IP.To4() == nil && !ua.IP.IsUnspecified() {
			network = "udp6"
		}
		if strings.Contains(pc.LocalAddr().String(), "[") {
			network = "udp6"
		}
		e.mu.Lock()
		e.dhts = append(e.dhts, &dhtNode{s: s, network: network})
		e.mu.Unlock()
	}
}

func (e *Engine) nodesFile(network string) string {
	if strings.HasSuffix(network, "6") {
		return e.paths.file("dht6.nodes")
	}
	return e.paths.file("dht4.nodes")
}

// dhtStartingNodes: сохраненные узлы плюс общеизвестные маршрутизаторы.
func (e *Engine) dhtStartingNodes(network string) dht.StartingNodesGetter {
	return func() ([]dht.Addr, error) {
		var out []dht.Addr
		if ns, err := dht.ReadNodesFromFile(e.nodesFile(network)); err == nil {
			for _, n := range ns {
				out = append(out, dht.NewAddr(n.Addr.UDP()))
			}
		}
		boot, err := dht.GlobalBootstrapAddrs(network)
		out = append(out, boot...)
		if len(out) == 0 {
			return nil, err
		}
		return out, nil
	}
}

func (e *Engine) saveDHTNodes(nodes []*dhtNode) {
	for _, d := range nodes {
		ns := d.s.Nodes()
		if len(ns) < 8 {
			continue // не затирать хороший файл почти пустой таблицей
		}
		if err := dht.WriteNodesToFile(ns, e.nodesFile(d.network)); err != nil {
			logf("Узлы DHT не сохранены: %v", err)
		}
	}
}

func (e *Engine) bootstrapDHT() {
	e.mu.Lock()
	ds := append([]*dhtNode(nil), e.dhts...)
	e.mu.Unlock()
	for _, d := range ds {
		go func(s *dht.Server) { _, _ = s.Bootstrap() }(d.s)
	}
}
