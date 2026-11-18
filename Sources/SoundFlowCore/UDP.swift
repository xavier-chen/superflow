import Foundation
import Darwin

final class UDPSocket {
    let fd: Int32
    private var source: DispatchSourceRead?
    init(port: UInt16, queue: DispatchQueue, multicast: Bool = false, receive: @escaping (Data, sockaddr_in) -> Void) throws {
        fd = Darwin.socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP)
        guard fd >= 0 else { throw POSIXError(.EIO) }
        do {
            var yes: Int32 = 1
            // Do not enable SO_REUSEPORT: two receiver instances must not steal each other's packets.
            setsockopt(fd, SOL_SOCKET, SO_BROADCAST, &yes, socklen_t(MemoryLayout.size(ofValue: yes)))
            var address = Self.address("0.0.0.0", port: port)
            let result = withUnsafePointer(to: &address) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
            }
            guard result == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
            _ = fcntl(fd, F_SETFL, O_NONBLOCK)
            if multicast {
                var membership = ip_mreq()
                membership.imr_multiaddr.s_addr = inet_addr(Wire.multicast)
                membership.imr_interface.s_addr = INADDR_ANY
                setsockopt(fd, IPPROTO_IP, IP_ADD_MEMBERSHIP, &membership, socklen_t(MemoryLayout<ip_mreq>.size))
            }
            let read = DispatchSource.makeReadSource(fileDescriptor: fd, queue: queue)
            let descriptor = fd
            read.setEventHandler {
                var bytes = [UInt8](repeating: 0, count: 2048)
                // Bound work so packet floods cannot starve the playout timer or stop request.
                for _ in 0..<256 {
                    var remote = sockaddr_in()
                    var length = socklen_t(MemoryLayout<sockaddr_in>.size)
                    let count = withUnsafeMutablePointer(to: &remote) { pointer in
                        pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                            recvfrom(descriptor, &bytes, bytes.count, 0, $0, &length)
                        }
                    }
                    if count < 0 { break }
                    if count > 0 { receive(Data(bytes.prefix(count)), remote) }
                }
            }
            read.setCancelHandler { Darwin.close(descriptor) }
            source = read
            read.resume()
        } catch { Darwin.close(fd); throw error }
    }
    func send(_ data: Data, to remote: sockaddr_in) {
        var destination = remote
        data.withUnsafeBytes { bytes in
            _ = withUnsafePointer(to: &destination) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { sendto(fd, bytes.baseAddress, bytes.count, 0, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
            }
        }
    }
    func close() { source?.cancel(); source = nil }
    deinit { close() }
    static func address(_ ip: String, port: UInt16) -> sockaddr_in {
        var value = sockaddr_in()
        value.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        value.sin_family = sa_family_t(AF_INET)
        value.sin_port = port.bigEndian
        value.sin_addr.s_addr = inet_addr(ip)
        return value
    }
    static func host(_ address: sockaddr_in) -> String {
        var raw = address.sin_addr
        var output = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
        _ = inet_ntop(AF_INET, &raw, &output, socklen_t(output.count))
        return String(cString: output)
    }
}
