// Probe: does recover() accept the high-S twin that Go accepts?
use lux_platformvm::sign::{address, recover};

fn hexb(s: &str) -> Vec<u8> {
    (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i+2],16).unwrap()).collect()
}

#[test]
fn high_s_probe() {
    let hash: [u8;32] = hexb("0707070707070707070707070707070707070707070707070707070707070707").try_into().unwrap();
    let low: [u8;65]  = hexb("111f20b9521ba1924ecfb91595426246b152cc1187e83f798cbd61f95f2c4cb107da8d209539506429d1ecd4033b2c207b89267f7dd8674421737193cd84f1dc00").try_into().unwrap();
    let high: [u8;65] = hexb("111f20b9521ba1924ecfb91595426246b152cc1187e83f798cbd61f95f2c4cb1f82572df6ac6af9bd62e132bfcc4d3de3f25b667317038f79e5eecf902b14f6501").try_into().unwrap();
    let gokey = hexb("034f355bdcb7cc0af728ef3cceb9615d90684bb5b2ca5f859ab0f0b704075871aa");
    let goaddr = address(&gokey);

    println!("RUST_LOWS  {:?}", recover(&hash, &low).map(|a| hex(&a.0)));
    println!("RUST_HIGHS {:?}", recover(&hash, &high).map(|a| hex(&a.0)));
    println!("GO_ADDR    {}", hex(&goaddr.0));
    assert_eq!(recover(&hash,&low), Some(goaddr), "low-S must agree with Go");
    // The question under test:
    match recover(&hash,&high) {
        Some(a) => println!("VERDICT: RUST ACCEPTS HIGH-S -> {} (agrees with Go)", hex(&a.0)),
        None    => println!("VERDICT: RUST REJECTS HIGH-S -- DIVERGENCE FROM GO"),
    }
}

fn hex(b:&[u8])->String{ b.iter().map(|x|format!("{:02x}",x)).collect() }
