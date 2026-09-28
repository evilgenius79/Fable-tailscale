package sysmetrics

import (
	"net/netip"
	"os"
	"runtime"
	"strings"
)

// Tailscale address ranges (https://tailscale.com/kb/1015/100.x-addresses).
var (
	tailscaleCGNAT = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleULA   = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// IsTailscaleAddr reports whether a is inside Tailscale's IPv4 CGNAT range
// or its IPv6 ULA range.
func IsTailscaleAddr(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap()
	return tailscaleCGNAT.Contains(a) || tailscaleULA.Contains(a)
}

// pseudoFSTypes are filesystem types that never hold user data.
var pseudoFSTypes = map[string]struct{}{
	"squashfs": {}, "tmpfs": {}, "devtmpfs": {}, "overlay": {}, "overlayfs": {},
	"proc": {}, "procfs": {}, "sysfs": {}, "cgroup": {}, "cgroup2": {}, "cgroupfs": {},
	"autofs": {}, "devpts": {}, "devfs": {}, "mqueue": {}, "debugfs": {}, "tracefs": {},
	"securityfs": {}, "pstore": {}, "bpf": {}, "configfs": {}, "hugetlbfs": {},
	"fusectl": {}, "binfmt_misc": {}, "ramfs": {}, "efivarfs": {}, "nsfs": {},
	"rpc_pipefs": {}, "selinuxfs": {}, "fdescfs": {}, "linprocfs": {}, "linsysfs": {},
	"kernfs": {}, "ptyfs": {}, "nullfs": {}, "iso9660": {}, "udf": {}, "cdfs": {},
	"portal": {}, "sockfs": {}, "pipefs": {}, "anon_inodefs": {}, "none": {},
	"snap": {}, "vfat_efi": {}, "apfs_snapshot": {},
	// Network filesystems: statfs on a stale mount can block indefinitely
	// and is not cancellable, which would stall the whole sampler.
	"nfs": {}, "nfs4": {}, "nfsd": {}, "cifs": {}, "smbfs": {}, "smb3": {}, "afs": {},
	"9p": {}, "ceph": {}, "glusterfs": {}, "lustre": {}, "afpfs": {}, "webdav": {},
	"fuse.sshfs": {}, "fuse.rclone": {}, "fuse.gvfsd-fuse": {}, "fuse.portal": {},
}

// allowedFuseTypes are FUSE filesystems that are worth reporting despite
// the general FUSE exclusion (Docker Desktop and similar host shares that
// behave like local disks).
var allowedFuseTypes = map[string]struct{}{
	"fuse.osxfs": {}, "fuse.grpcfuse": {}, "fuse.virtiofs": {}, "virtiofs": {},
	"fuse.zfs": {}, "fuse.ntfs": {}, "fuse.ntfs-3g": {}, "fuseblk": {}, "ntfs3": {},
}

// pseudoMountPrefixes are mount point prefixes for kernel, container and
// firmware mounts that are excluded regardless of filesystem type.
var pseudoMountPrefixes = []string{
	"/proc", "/sys", "/dev", "/run", "/snap", "/boot/efi",
	"/var/lib/docker", "/var/lib/containers", "/var/lib/containerd",
	"/var/lib/kubelet", "/var/snap", "/System/Volumes", "/private/var/vm",
}

// KeepPartition reports whether a mounted filesystem should be included in
// the report. Pseudo, container, firmware and network filesystems are
// excluded by type and by mount-point prefix; "/boot" is kept.
func KeepPartition(mount, fstype string) bool {
	mount = strings.TrimSpace(mount)
	if mount == "" {
		return false
	}
	ft := strings.ToLower(strings.TrimSpace(fstype))
	if _, deny := pseudoFSTypes[ft]; deny {
		return false
	}
	if strings.HasPrefix(ft, "fuse") {
		if _, ok := allowedFuseTypes[ft]; !ok {
			return false
		}
	}
	if runtime.GOOS == "windows" {
		return true
	}
	for _, p := range pseudoMountPrefixes {
		if mount == p || strings.HasPrefix(mount, p+"/") {
			return false
		}
	}
	return true
}

// IsPrimaryMount reports whether mount is the root/system volume: "/" on
// Unix-like systems, the system drive (usually "C:\") on Windows.
func IsPrimaryMount(mount string) bool {
	if runtime.GOOS != "windows" {
		return mount == "/"
	}
	drive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if drive == "" {
		drive = "C:"
	}
	m := strings.TrimRight(strings.TrimSpace(mount), `\/`)
	return strings.EqualFold(m, strings.TrimRight(drive, `\/`))
}

// virtualIfacePrefixes are interface name prefixes (case-insensitive) that
// mark loopback, VPN, container and bridge interfaces as non-physical.
var virtualIfacePrefixes = []string{
	"lo", "tailscale", "utun", "wg", "docker", "br-", "veth", "virbr", "vmnet",
	"ts", "zt", "tun", "tap", "cni", "flannel", "podman", "bridge", "awdl", "llw",
	"gif", "stf", "anpi", "ap1", "dummy", "ifb", "lxc", "lxd", "kube", "cali",
	"vethernet", "nerdctl", "ipsec", "gre", "sit", "ip6tnl", "erspan", "vxlan",
	"vboxnet", "hyper-v", "isatap", "teredo", "6to4",
}

// IsPhysicalInterface applies the physical-interface heuristic from the
// package contract: an interface is physical unless its name starts with a
// known loopback/VPN/container/bridge prefix or contains "loopback".
func IsPhysicalInterface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if strings.Contains(n, "loopback") {
		return false
	}
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) {
			return false
		}
	}
	return true
}

// InterfaceAddrs is one interface with its addresses (CIDR or bare IP
// strings) as input to DetectTailscaleInterface.
type InterfaceAddrs struct {
	Name  string
	Addrs []string
}

// tailscaleIfacePrefixes are the interface names tailscaled uses for its
// TUN device: tailscale0 (Linux/FreeBSD), utunN (macOS), "Tailscale"
// (Windows).
var tailscaleIfacePrefixes = []string{"tailscale", "utun"}

// DetectTailscaleInterface returns the name of the first interface whose
// name starts with "tailscale" or "utun" (case-insensitive) and that carries
// a Tailscale address. When no interface matches by name, the first
// non-loopback interface carrying a Tailscale address is returned (custom
// --tun names). It returns "" when nothing matches.
func DetectTailscaleInterface(ifaces []InterfaceAddrs) string {
	var fallback string
	for _, i := range ifaces {
		if !hasTailscaleAddr(i.Addrs) {
			continue
		}
		n := strings.ToLower(i.Name)
		for _, p := range tailscaleIfacePrefixes {
			if strings.HasPrefix(n, p) {
				return i.Name
			}
		}
		if fallback == "" && !strings.HasPrefix(n, "lo") {
			fallback = i.Name
		}
	}
	return fallback
}

// hasTailscaleAddr reports whether any address (CIDR or bare) is a
// Tailscale address.
func hasTailscaleAddr(addrs []string) bool {
	for _, s := range addrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		var a netip.Addr
		if p, err := netip.ParsePrefix(s); err == nil {
			a = p.Addr()
		} else if v, err := netip.ParseAddr(s); err == nil {
			a = v
		} else {
			continue
		}
		if IsTailscaleAddr(a) {
			return true
		}
	}
	return false
}

// trimSpaces collapses interior whitespace runs and trims the ends (CPU
// model strings often contain runs of spaces).
func trimSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
