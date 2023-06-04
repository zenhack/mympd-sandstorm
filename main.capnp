@0x97dd53ba62adf025;

using Go = import "/go.capnp";
$Go.package("main");
$Go.import("zenhack.net/go/mympd-proxy");

struct Config {
  host @0 :Text;
  port @1 :UInt16;
}
