// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! An instant, read and written the way the Go chain reads and writes one.
//!
//! `time.Time` marshals as RFC 3339 with the fraction trimmed of trailing
//! zeros and dropped entirely when it is zero, and with the OFFSET it was
//! parsed under — not normalised to UTC. Both halves are the wire: a block
//! whose timestamp reads `+05:30` is re-marshalled as `+05:30`, and an
//! implementation that normalised it would hash a different block.
//!
//! The instant is kept as the offset-local civil fields it was written in
//! rather than as a count from the epoch, because that is what survives a
//! round trip exactly. The epoch second is derived when a rule needs one.

#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct Time {
    pub year: i64,
    pub month: u32,
    pub day: u32,
    pub hour: u32,
    pub minute: u32,
    pub second: u32,
    pub nanos: u32,
    /// Seconds east of UTC. Zero is written `Z`.
    pub offset: i32,
}

fn days_from_civil(y: i64, m: u32, d: u32) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m as i64 + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d as i64 - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

fn days_in_month(y: i64, m: u32) -> u32 {
    match m {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if (y % 4 == 0 && y % 100 != 0) || y % 400 == 0 {
                29
            } else {
                28
            }
        }
        _ => 0,
    }
}

impl Time {
    /// Seconds since the epoch, UTC.
    pub fn unix(&self) -> i64 {
        days_from_civil(self.year, self.month, self.day) * 86400
            + self.hour as i64 * 3600
            + self.minute as i64 * 60
            + self.second as i64
            - self.offset as i64
    }

    pub fn unix_nanos(&self) -> i64 {
        self.unix() * 1_000_000_000 + self.nanos as i64
    }

    /// RFC 3339, Go's way: the fraction carries no trailing zeros and vanishes
    /// when it is zero, and a zero offset is the letter Z.
    pub fn render(&self) -> String {
        let mut s = format!(
            "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}",
            self.year, self.month, self.day, self.hour, self.minute, self.second
        );
        if self.nanos != 0 {
            let mut frac = format!("{:09}", self.nanos);
            while frac.ends_with('0') {
                frac.pop();
            }
            s.push('.');
            s.push_str(&frac);
        }
        if self.offset == 0 {
            s.push('Z');
        } else {
            let (sign, off) = if self.offset < 0 {
                ('-', -self.offset)
            } else {
                ('+', self.offset)
            };
            s.push(sign);
            s.push_str(&format!("{:02}:{:02}", off / 3600, (off % 3600) / 60));
        }
        s
    }

    /// RFC 3339, strictly: four-digit year, `T`, and either `Z` or `±HH:MM`.
    /// A fraction is any number of digits and is truncated at nine, which is
    /// what the Go parser does with the tenth.
    pub fn parse(s: &str) -> Result<Time, String> {
        let bad = || format!("parsing time {s:?} as RFC 3339: cannot parse");
        let b = s.as_bytes();
        if b.len() < 20 {
            return Err(bad());
        }
        let num = |from: usize, to: usize| -> Result<i64, String> {
            let part = &s[from..to];
            if !part.bytes().all(|c| c.is_ascii_digit()) {
                return Err(bad());
            }
            part.parse::<i64>().map_err(|_| bad())
        };
        if b[4] != b'-' || b[7] != b'-' || b[10] != b'T' || b[13] != b':' || b[16] != b':' {
            return Err(bad());
        }
        let mut t = Time {
            year: num(0, 4)?,
            month: num(5, 7)? as u32,
            day: num(8, 10)? as u32,
            hour: num(11, 13)? as u32,
            minute: num(14, 16)? as u32,
            second: num(17, 19)? as u32,
            nanos: 0,
            offset: 0,
        };
        let mut i = 19;
        if i < b.len() && b[i] == b'.' {
            i += 1;
            let start = i;
            while i < b.len() && b[i].is_ascii_digit() {
                i += 1;
            }
            if i == start {
                return Err(bad());
            }
            let digits = &s[start..i];
            let mut nanos: u64 = 0;
            for (k, c) in digits.bytes().enumerate() {
                if k < 9 {
                    nanos = nanos * 10 + (c - b'0') as u64;
                }
            }
            for _ in digits.len()..9 {
                nanos *= 10;
            }
            t.nanos = nanos as u32;
        }
        if i >= b.len() {
            return Err(bad());
        }
        match b[i] {
            b'Z' => {
                i += 1;
            }
            b'+' | b'-' => {
                if i + 6 > b.len() || b[i + 3] != b':' {
                    return Err(bad());
                }
                let hh = num(i + 1, i + 3)?;
                let mm = num(i + 4, i + 6)?;
                if hh > 23 || mm > 59 {
                    return Err(bad());
                }
                let off = (hh * 3600 + mm * 60) as i32;
                t.offset = if b[i] == b'-' { -off } else { off };
                i += 6;
            }
            _ => return Err(bad()),
        }
        if i != b.len() {
            return Err(bad());
        }
        if t.month < 1 || t.month > 12 || t.day < 1 || t.day > days_in_month(t.year, t.month) {
            return Err(format!("parsing time {s:?}: day out of range"));
        }
        if t.hour > 23 || t.minute > 59 || t.second > 59 {
            return Err(bad());
        }
        Ok(t)
    }

    /// The zero `time.Time`, which Go writes as year one at midnight UTC.
    pub fn zero() -> Time {
        Time { year: 1, month: 1, day: 1, hour: 0, minute: 0, second: 0, nanos: 0, offset: 0 }
    }
}
