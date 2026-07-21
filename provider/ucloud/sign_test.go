package ucloud

import "testing"

func TestSign(t *testing.T) {
	values := map[string]string{
		"Action": "CreateUHostInstance", "CPU": "2", "ChargeType": "Month",
		"DiskSpace": "10", "ImageId": "f43736e1-65a5-4bea-ad2e-8a46e18883c2",
		"LoginMode": "Password", "Memory": "2048", "Name": "Host01",
		"Password": "VUNsb3VkLmNu", "PublicKey": "ucloudsomeone@example.com1296235120854146120",
		"Quantity": "1", "Region": "cn-bj2", "SecurityToken": "some_stoken",
		"Zone": "cn-bj2-04",
	}

	got := sign(values, "46f09bb9fab4f12dfc160dae12273d5332b5debe")
	if got != "170c480ad176a247b324eb92a2cfe536aacfbd04" {
		t.Fatalf("signature=%s", got)
	}
}
