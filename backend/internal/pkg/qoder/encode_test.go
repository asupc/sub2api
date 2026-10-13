//go:build unit

package qoder

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// encodeGolden 全部 43 条黄金向量由源实现 qoder_sign.qoder_encode 生成：
// 长度 0..39 逐长度递增（覆盖三段轮转在 mod 3 各余数下的切分）+
// 随机字节 + UTF-8 中文 + 100 字节长输入。
var encodeGolden = []struct{ plainHex, enc string }{
	{"", ""},
	{"00", "$_$_"},
	{"0001", "$_T_"},
	{"000102", "o_T_"},
	{"00010203", "$$To_f__"},
	{"0001020304", "p$To_f__"},
	{"000102030405", "pgTo_f__"},
	{"00010203040506", "d&$$_fpg__To"},
	{"0001020304050607", "d&N$_fpg__To"},
	{"000102030405060708", "d&NB_fpg__To"},
	{"00010203040506070809", "Bop$$fpgd&N__To_"},
	{"000102030405060708090a", "Bopn$fpgd&N__To_"},
	{"000102030405060708090a0b", "BopnGfpgd&N__To_"},
	{"000102030405060708090a0b0c", "nGR_$$pgd&NBop__To_f"},
	{"000102030405060708090a0b0c0d", "nGR_w$pgd&NBop__To_f"},
	{"000102030405060708090a0b0c0d0e", "nGR_wlpgd&NBop__To_f"},
	{"000102030405060708090a0b0c0d0e0f", "R_wlRf$$d&NBopnG__To_fpg"},
	{"000102030405060708090a0b0c0d0e0f10", "R_wlR%_$d&NBopnG__To_fpg"},
	{"000102030405060708090a0b0c0d0e0f1011", "R_wlR%_Cd&NBopnG__To_fpg"},
	{"000102030405060708090a0b0c0d0e0f101112", "lR%_CT&$$&NBopnGR_w__To_fpgd"},
	{"000102030405060708090a0b0c0d0e0f10111213", "lR%_CTiV$&NBopnGR_w__To_fpgd"},
	{"000102030405060708090a0b0c0d0e0f1011121314", "lR%_CTiVa&NBopnGR_w__To_fpgd"},
	{"000102030405060708090a0b0c0d0e0f101112131415", "_CTiVagp$$NBopnGR_wlR%__To_fpgd&"},
	{"000102030405060708090a0b0c0d0e0f10111213141516", "_CTiVagC#$NBopnGR_wlR%__To_fpgd&"},
	{"000102030405060708090a0b0c0d0e0f1011121314151617", "_CTiVagC#xNBopnGR_wlR%__To_fpgd&"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718", "TiVagC#xH_$$opnGR_wlR%_C__To_fpgd&NB"},
	{"000102030405060708090a0b0c0d0e0f10111213141516171819", "TiVagC#xHdz$opnGR_wlR%_C__To_fpgd&NB"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a", "TiVagC#xHdzPopnGR_wlR%_C__To_fpgd&NB"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b", "agC#xHdzPHf$$pnGR_wlR%_CTiV__To_fpgd&NBo"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c", "agC#xHdzPH%f$pnGR_wlR%_CTiV__To_fpgd&NBo"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d", "agC#xHdzPH%fJpnGR_wlR%_CTiV__To_fpgd&NBo"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e", "#xHdzPH%fJZ&$$nGR_wlR%_CTiVagC__To_fpgd&NBop"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", "#xHdzPH%fJZiq$nGR_wlR%_CTiVagC__To_fpgd&NBop"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", "#xHdzPH%fJZiq&nGR_wlR%_CTiVagC__To_fpgd&NBop"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2021", "HdzPH%fJZiq&Bp$$R_wlR%_CTiVagC#x__To_fpgd&NBopnG"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122", "HdzPH%fJZiq&B,B$R_wlR%_CTiVagC#x__To_fpgd&NBopnG"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20212223", "HdzPH%fJZiq&B,BMR_wlR%_CTiVagC#x__To_fpgd&NBopnG"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2021222324", "PH%fJZiq&B,BMK_$$_wlR%_CTiVagC#xHdz__To_fpgd&NBopnGR"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425", "PH%fJZiq&B,BMKoa$_wlR%_CTiVagC#xHdz__To_fpgd&NBopnGR"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20212223242526", "PH%fJZiq&B,BMKoaO_wlR%_CTiVagC#xHdz__To_fpgd&NBopnGR"},
	{"00ff109cde", "I$qpEj_v"},
	{"e4b8ade69687e6b58be8af95414243313233", "paKRV@B^QUSGycAFQG*tQ)PZ"},
	{"61616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161", "i#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#p$$Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sg#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#Sgi#"},
}

func TestQoderEncodeGolden(t *testing.T) {
	for _, v := range encodeGolden {
		plain, err := hex.DecodeString(v.plainHex)
		if err != nil {
			t.Fatalf("bad hex %q: %v", v.plainHex, err)
		}
		if got := QoderEncode(plain); got != v.enc {
			t.Fatalf("QoderEncode(%s) = %q, want %q", v.plainHex, got, v.enc)
		}
	}
	if got := QoderEncode([]byte("{}")); got != "$kwm" {
		t.Fatalf("encode({}) = %q, want $kwm", got)
	}
}

func TestQoderEncodeDecodeRoundTrip(t *testing.T) {
	for n := 0; n < 120; n++ {
		plain := make([]byte, n)
		for i := range plain {
			plain[i] = byte(i * 7 % 256)
		}
		enc := QoderEncode(plain)
		back, err := QoderDecode(enc)
		if err != nil {
			t.Fatalf("decode(%d bytes) error: %v", n, err)
		}
		if !bytes.Equal(back, plain) {
			t.Fatalf("roundtrip failed at len %d", n)
		}
	}
	// UTF-8 中文
	cn := []byte("中文测试 with mixed ASCII 123!@#")
	back, err := QoderDecode(QoderEncode(cn))
	if err != nil || !bytes.Equal(back, cn) {
		t.Fatalf("unicode roundtrip failed: %v %q", err, back)
	}
}

func TestQoderDecodeRejectsInvalidChars(t *testing.T) {
	if _, err := QoderDecode("ab/d"); err == nil {
		t.Fatal("'/' is not in the custom alphabet; must error")
	}
	if _, err := QoderDecode("ab="); err == nil {
		t.Fatal("'=' is not valid encoded output (pad is $); must error")
	}
	if _, err := QoderDecode("$$$"); err == nil {
		t.Fatal("invalid length must error")
	}
}

func TestCustomAlphabetShape(t *testing.T) {
	if len(qoderCustomAlphabet) != 64 || len(qoderStdAlphabet) != 64 {
		t.Fatal("alphabet length")
	}
	seen := map[byte]bool{}
	for i := 0; i < len(qoderCustomAlphabet); i++ {
		c := qoderCustomAlphabet[i]
		if seen[c] {
			t.Fatalf("duplicate %q at %d", string(c), i)
		}
		seen[c] = true
	}
}
