// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"fmt"

	"github.com/luxfi/node/config"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/utils/crypto/secp256k1"
)

func LocalNetworkOrPanic() *Network {
	// Temporary network configured with local network keys
	// See: /staking/local/README.md
	network := &Network{
		NetworkID: genesis.LocalConfig.NetworkID,
		PreFundedKeys: []*secp256k1.PrivateKey{
			genesis.EWOQKey, // Funded in the local genesis
		},
		Nodes: []*Node{
			{
				Flags: FlagsMap{
					config.StakingTLSKeyContentKey:    "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKS0FJQkFBS0NBZ0VBeW1Fa2NQMXRHS1dCL3pFMElZaGEwZEp2UFplc2s3c3k2UTdZN25hLytVWjRTRDc3CmFwTzJDQnB2NXZaZHZjY0VlQ2VhKzBtUnJQdTlNZ1hyWkcwdm9lejhDZHE1bGc4RzYzY2lTcjFRWWFuL0pFcC8KbFZOaTNqMGlIQ1k5ZmR5dzhsb1ZkUitKYWpaRkpIQUVlK2hZZmFvekx3TnFkTHlldXZuZ3kxNWFZZjBXR3FVTQpmUjREby85QVpnQ2pLMkFzcU9RWVVZb2Zqcm9JUEdpdUJ2VDBFeUFPUnRzRTFsdGtKQjhUUDBLYVJZMlhmMThFCkhpZGgrcm0xakJYT1g3YlgrZ002U2J4U0F3YnZ5UXdpbG9ncnVadkxlQmkvTU5qcXlNZkNiTmZaUmVHR0JObnEKSXdxM3FvRDR1dUV0NkhLc0NyQTZNa2s4T3YrWWlrT1FWR01GRjE5OCt4RnpxZy9FakIzbjFDbm5NNCtGcndHbQpTODBkdTZsNXVlUklFV0VBQ0YrSDRabU96WDBxS2Qxb2RCS090dmlSNkRQOVlQbElEbDVXNTFxV1BlVElIZS8zCjhBMGxpN3VDTVJOUDdxdkZibnlHM3d1TXEyUEtwVTFYd0gzeU5aWFVYYnlZenlRVDRrTkFqYXpwZXRDMWFiYVoKQm5QYklSKzhHZG16OUd4SjJDazRDd0h6c3cvRkxlOVR0Z0RpR3ZOQU5SalJaZUdKWHZ6RWpTVG5FRGtxWUxWbgpVUk15RktIcHdJMzdzek9Ebms2K0hFWU9QbFdFd0tQU2h5cTRqZFE3bnNEY3huZkZveWdGUjVuQ0RJNmlFaTA1CmN6SVhiSFp2anBuME9qcjhsKzc5Qmt6Z1c0VDlQVFJuTU1PUU5JQXMxemRmQlV1YU1aOFh1amh2UTlNQ0F3RUEKQVFLQ0FnRUF1VU00TXQ4cjhiWUJUUFZqL1padlhVakFZS2ZxYWNxaWprcnpOMGtwOEM0Y2lqWnR2V0MrOEtnUwo3R0YzNnZTM0dLOVk1dFN3TUtTNnk0SXp2RmxmazJINFQ2VVU0MU9hU0E5bEt2b25EV0NybWpOQW5CZ2JsOHBxCjRVMzRXTEdnb2hycExiRFRBSkh4dGF0OXoxZ2hPZGlHeG5EZ0VVRmlKVlA5L3UyKzI1anRsVEttUGhzdHhnRXkKbUszWXNTcDNkNXhtenE0Y3VYRi9mSjF2UWhzWEhETHFIdDc4aktaWkErQVdwSUI1N1ZYeTY3eTFiazByR25USwp4eFJuT2FPT0R1YkpneHFNRVExV2tMczFKb3c5U3NwZDl2RGdoUHp0NFNOTXpvckI4WURFU01pYjE3eEY2aVhxCmpGajZ4NkhCOEg3bXA0WDNSeU1ZSnVvMnc2bHB6QnNFbmNVWXBLaHFNYWJGMEkvZ2lJNVZkcFNEdmtDQ09GZW4KbldaTFY5QWkveDd0VHEvMEYrY1ZNNjlNZ2ZlOGlZeW1xbGZkNldSWklUS2ZWaU5IQUxsRy9QcTl5SEpzejdOZwpTOEJLT0R0L3NqNFEweEx0RkRUL0RtcFA1MGlxN1NpUzE0b2JjS2NRcjhGQWpNL3NPWS9VbGc0TThNQTdFdWdTCnBESndMbDZYRG9JTU1DTndaMUhHc0RzdHpteDVNZjUwYlM0dGJLNGlaemNwUFg1UkJUbFZkbzlNVFNnbkZpenAKSWkxTmpITHVWVkNTTGIxT2pvVGd1MGNRRmlXRUJDa0MxWHVvUjhSQ1k2aVdWclVINEdlem5pN2NrdDJtSmFOQQpwZDYvODdkRktFM2poNVQ2alplSk1KZzVza1RaSFNvekpEdWFqOXBNSy9KT05TRDA2c0VDZ2dFQkFQcTJsRW1kCmcxaHBNSXFhN2V5MXVvTGQxekZGemxXcnhUSkxsdTM4TjY5bVlET0hyVi96cVJHT3BaQisxbkg3dFFKSVQvTDEKeExOMzNtRlZxQ3JOOHlVbVoraVVXaW9hSTVKWjFqekNnZW1WR2VCZ29kd1A5TU9aZnh4ckRwMTdvVGRhYmFFcQo3WmFCWW5ZOHhLLzRiQ3h1L0I0bUZpRjNaYThaVGQvKzJ5ZXY3Sk0rRTNNb3JXYzdyckttMUFwZmxmeHl0ZGhPCkpMQmlxT2Nxb2JJM2RnSHl6ZXNWYjhjVDRYQ3BvUmhkckZ3b3J0MEpJN3J5ZmRkZDQ5dk1KM0VsUmJuTi9oNEYKZjI0Y1dZL3NRUHEvbmZEbWVjMjhaN25WemExRDRyc3pOeWxZRHZ6ZGpGMFExbUw1ZEZWbnRXYlpBMUNOdXJWdwpuVGZ3dXlROFJGOVluWU1DZ2dFQkFNNmxwTmVxYWlHOWl4S1NyNjVwWU9LdEJ5VUkzL2VUVDR2Qm5yRHRZRis4Cm9oaUtnSXltRy92SnNTZHJ5bktmd0pPYkV5MmRCWWhDR0YzaDl6Mm5jOUtKUUQvc3U3d3hDc2RtQnM3WW9EaU0KdXpOUGxSQW1JMFFBRklMUENrNDh6L2xVUWszci9NenUwWXpSdjdmSTRXU3BJR0FlZlZQRHF5MXVYc0FURG9ESgphcmNFa05ENUxpYjg5THg3cjAyRWV2SkpUZGhUSk04bUJkUmw2d3BOVjN4QmR3aXM2N3VTeXVuRlpZcFNpTXc3CldXaklSaHpoTEl2cGdENzhVdk52dUppMFVHVkVqVHFueHZ1VzNZNnNMZklrODBLU1IyNFVTaW5UMjd0Ly94N3oKeXpOa283NWF2RjJobTFmOFkvRXBjSEhBYXg4TkFRRjV1dVY5eEJOdnYzRUNnZ0VBZFMvc1JqQ0syVU5wdmcvRwowRkx0V0FncmNzdUhNNEl6alZ2SnMzbWw2YVYzcC81dUtxQncwVlVVekdLTkNBQTRUbFhRa09jUnh6VnJTNkhICkZpTG4yT0NIeHkyNHExOUdhenowcDdmZkUzaHUvUE1PRlJlY04rVkNoZDBBbXRuVHRGVGZVMnNHWE1nalp0TG0KdUwzc2lpUmlVaEZKWE9FN05Vb2xuV0s1dTJZK3RXQlpwUVZKY0N4MGJ1c054NytBRXR6blpMQzU4M3hhS0p0RApzMUs3SlJRQjdqVTU1eHJDMEc5cGJrTXlzbTBOdHlGemd3bWZpcEJIVmxDcHl2ZzZEQ3hkOEZodmhOOVplYTFiCmZoa2MwU0pab3JIQzVoa3FweWRKRG1sVkNrMHZ6RUFlUU00Qzk0WlVPeXRibmpRbm1YcDE0Q05BU1lxTFh0ZVEKdWVSbzB3S0NBUUFHMEYxMEl4Rm0xV290alpxdlpKZ21RVkJYLzBmclVQY3hnNHZwQjVyQzdXUm03TUk2WVF2UgpMS0JqeldFYWtIdjRJZ2ZxM0IrZms1WmNHaVJkNnhTZG41cjN3S1djR2YzaC8xSkFKZEo2cXVGTld0VnVkK04zCnpZemZsMVllcUZDdlJ3RDhzc2hlTlkzQlYvVTdhU3ROZDJveTRTNSt3WmYyWW9wTFNSV1VWNC9tUXdkSGJNQUIKMXh0Mno1bEROQmdkdng4TEFBclpyY1pKYjZibGF4RjBibkF2WUF4UjNoQkV6eFovRGlPbW9GcGRZeVUwdEpRVQpkUG1lbWhGZUo1UHRyUnh0aW1vaHdnQ0VzVC9UQVlodVVKdVkyVnZ6bkVXcHhXdWNiaWNLYlQySkQwdDY3bUVCCnNWOSs4anFWYkNsaUJ0ZEJhZHRib2hqd2trb1IzZ0J4QW9JQkFHM2NadU5rSVdwRUxFYmVJQ0tvdVNPS04wNnIKRnMvVVhVOHJvTlRoUFI3dlB0amVEMU5ETW1VSEpyMUZHNFNKclNpZ2REOHFOQmc4dy9HM25JMEl3N2VGc2trNQo4bU5tMjFDcER6T04zNlpPN0lETWo1dXlCbGoydCtJeGwvdUpZaFlTcHVOWHlVVE1tK3JrRkowdmRTVjRmakxkCkoybTMwanVZbk1pQkJKZjdkejVNOTUrVDB4aWNHV3lWMjR6VllZQmJTbzBOSEVHeHFlUmhpa05xWk5Qa29kNmYKa2ZPSlpHYWxoMkthSzVSTXBacEZGaFova1c5eFJXTkpaeUNXZ2tJb1lrZGlsTXVJU0J1M2xDcms4cmRNcEFMMAp3SEVjcTh4d2NnWUNTMnFrOEh3anRtVmQzZ3BCMXk5VXNoTXIzcW51SDF3TXBVNUMrbk0yb3kzdlNrbz0KLS0tLS1FTkQgUlNBIFBSSVZBVEUgS0VZLS0tLS0K",
					config.StakingCertContentKey:      "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZOekNDQXg4Q0NRQzY4N1hGeHREUlNqQU5CZ2txaGtpRzl3MEJBUXNGQURCL01Rc3dDUVlEVlFRR0V3SlYKVXpFTE1Ba0dBMVVFQ0F3Q1Rsa3hEekFOQmdOVkJBY01Ca2wwYUdGallURVFNQTRHQTFVRUNnd0hRWFpoYkdGaQpjekVPTUF3R0ExVUVDd3dGUjJWamEyOHhEREFLQmdOVkJBTU1BMkYyWVRFaU1DQUdDU3FHU0liM0RRRUpBUllUCmMzUmxjR2hsYmtCaGRtRnNZV0p6TG05eVp6QWdGdzB4T1RBM01ESXhOakV5TVRWYUdBOHpNREU1TURjeE1ERTIKTVRJeE5Wb3dPakVMTUFrR0ExVUVCaE1DVlZNeEN6QUpCZ05WQkFnTUFrNVpNUkF3RGdZRFZRUUtEQWRCZG1GcwpZV0p6TVF3d0NnWURWUVFEREFOaGRtRXdnZ0lpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElDRHdBd2dnSUtBb0lDCkFRREtZU1J3L1cwWXBZSC9NVFFoaUZyUjBtODlsNnlUdXpMcER0anVkci81Um5oSVB2dHFrN1lJR20vbTlsMjkKeHdSNEo1cjdTWkdzKzcweUJldGtiUytoN1B3SjJybVdEd2JyZHlKS3ZWQmhxZjhrU24rVlUyTGVQU0ljSmoxOQozTER5V2hWMUg0bHFOa1VrY0FSNzZGaDlxak12QTJwMHZKNjYrZURMWGxwaC9SWWFwUXg5SGdPai8wQm1BS01yCllDeW81QmhSaWgrT3VnZzhhSzRHOVBRVElBNUcyd1RXVzJRa0h4TS9RcHBGalpkL1h3UWVKMkg2dWJXTUZjNWYKdHRmNkF6cEp2RklEQnUvSkRDS1dpQ3U1bTh0NEdMOHcyT3JJeDhKczE5bEY0WVlFMmVvakNyZXFnUGk2NFMzbwpjcXdLc0RveVNUdzYvNWlLUTVCVVl3VVhYM3o3RVhPcUQ4U01IZWZVS2Vjemo0V3ZBYVpMelIyN3FYbTU1RWdSCllRQUlYNGZobVk3TmZTb3AzV2gwRW82MitKSG9NLzFnK1VnT1hsYm5XcFk5NU1nZDcvZndEU1dMdTRJeEUwL3UKcThWdWZJYmZDNHlyWThxbFRWZkFmZkkxbGRSZHZKalBKQlBpUTBDTnJPbDYwTFZwdHBrR2M5c2hIN3daMmJQMApiRW5ZS1RnTEFmT3pEOFV0NzFPMkFPSWE4MEExR05GbDRZbGUvTVNOSk9jUU9TcGd0V2RSRXpJVW9lbkFqZnV6Ck00T2VUcjRjUmc0K1ZZVEFvOUtIS3JpTjFEdWV3TnpHZDhXaktBVkhtY0lNanFJU0xUbHpNaGRzZG0rT21mUTYKT3Z5WDd2MEdUT0JiaFAwOU5HY3d3NUEwZ0N6WE4xOEZTNW94bnhlNk9HOUQwd0lEQVFBQk1BMEdDU3FHU0liMwpEUUVCQ3dVQUE0SUNBUUFxTDFUV0kxUFRNbTNKYVhraGRUQmU4dHNrNytGc0hBRnpUY0JWQnNCOGRrSk5HaHhiCmRsdTdYSW0rQXlHVW4wajhzaXo4cW9qS2JPK3JFUFYvSW1USDVXN1EzNnJYU2Rndk5VV3BLcktJQzVTOFBVRjUKVDRwSCtscFlJbFFIblRhS011cUgzbk8zSTQwSWhFaFBhYTJ3QXd5MmtEbHo0NmZKY3I2YU16ajZaZzQzSjVVSwpaaWQrQlFzaVdBVWF1NVY3Q3BDN0dNQ3g0WWRPWldXc1QzZEFzdWc5aHZ3VGU4MWtLMUpvVEgwanV3UFRCSDB0CnhVZ1VWSVd5dXdlTTFVd1lGM244SG13cTZCNDZZbXVqaE1ES1QrM2xncVp0N2VaMVh2aWVMZEJSbFZRV3pPYS8KNlFZVGtycXdQWmlvS0lTdHJ4VkdZams0MHFFQ05vZENTQ0l3UkRnYm5RdWJSV3Jkc2x4aUl5YzVibEpOdU9WKwpqZ3Y1ZDJFZVVwd1VqdnBadUVWN0ZxUEtHUmdpRzBqZmw2UHNtczlnWVVYZCt5M3l0RzlIZW9ETm1MVFNUQkU0Cm5DUVhYOTM1UDIveE91b2s2Q3BpR3BQODlEWDd0OHlpd2s4TEZOblkzcnZ2NTBuVnk4a2VyVmRuZkhUbW9NWjkKL0lCZ29qU0lLb3Y0bG1QS2RnekZmaW16aGJzc1ZDYTRETy9MSWhURjdiUWJIMXV0L09xN25wZE9wTWpMWUlCRQo5bGFndlJWVFZGd1QvdXdyQ2NYSENiMjFiL3B1d1Y5NFNOWFZ3dDdCaGVGVEZCZHR4SnJSNGpqcjJUNW9kTGtYCjZuUWNZOFYyT1Q3S094bjBLVmM2cGwzc2FKVExtTCtILzNDdEFhbzlOdG11VURhcEtJTlJTVk55dmc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
					config.StakingSignerKeyContentKey: "QXZhbGFuY2hlTG9jYWxOZXR3b3JrVmFsaWRhdG9yMDE=",
				},
			},
			{
				Flags: FlagsMap{
					config.StakingTLSKeyContentKey:    "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKS2dJQkFBS0NBZ0VBM1U2RWV0SjJ1amJrZllrZ0ZETXN6MXlUVEZsVUd5OGsrZjRtUW9pZHdRZUdGakFZClg4YVNWaVlsbDNEUHM1dzRLU2JzZ2hIb2VRNmVtV054WHU1T2g0YmVoWmhhUTQ4SjQ2VUpuWkJDbElOTkJ2aEoKVDA2ZjFMSnJEeS9pUUxNRDZnV1ZxVEFVdDNiRjBEYnB6RExUQnBhMytVdEplbDErTXU1SFB4azhXSUwzbG1mUwpiVWY1dGlqWHEvc0k0QVI2aWVLejZweHdkOUxBMkQyWVBkUXM5UUpQQVc5c05pTmRQTTBGWUNpdW5pS2pTbHlTCmYrS1lzMG1YVk9QZHRNRE1XQWFaM3d3SElaOFhvdlZQNzkwRzArM2lsQ05ueHEvb1Rsa3lGbFBOUGxYamdiZkcKdWorTEV2UUVJZWYraHZLc0pnS2p0MkVQWGNUZk5vcnZ4OS9qZmhobXZ6TksrYXhGcTg4Q0xoWWNDcjhrSEtUMApRWnRza0p3WjRTL1J1WkRlcCtEZ1ROQ3JJR2R3bUpSMlVvT0hsbkRZQWJkVmdoYkVITzczMUJXaVo2L0VEOTZSCmVGZitBYUpPOEV6dVJ6ZEFOUkdCRldMbCtkelJwNnlsWHZoWVBmc1REb0Z6MXBGRGFuYjZTUFdXMWQyZEV1Zk8KdXFoZC83d1dhNFlBcXluT1JQN3pIc1FVWHZDb3VNQ3FqalZZWXZWaGxIRVI3eFJJcEhQdTFhejQ5R0dodXMxUwozZnJqZ0NUZk5YeWpqMktPRitMR2F6TWoySmR6b3hyVElYMWVDcndtaXhLOVhianBKYWp1bkFyeGtQSWdKUzFjCnQxU2NsdVJpZEE5Q3NvZ0lxK0ZUWFFDU3FOV1lJaG9yeVMxcnBVMzQxSW53amZhTmRlUUlxaWNZcXNNQ0F3RUEKQVFLQ0FnQU5HVU9nSFdybmxLNHJlLzFKRk1wWEw2eU1QVkZNRnB0Q3JMZEpBdHNMZk0yRDdLN1VwR1V1OGkwUgpiSnp1alpXSllnTm5vM1cyREpaNGo3azdIREhMdGNEZitXZUdUaVlRc2trQ2FYSjNaZG9lU24zVVV0d0U4OWFBClhKNHdwQ2ZjSng1M21CL3h4L2JuWHdpeGpHU1BKRWFaVzhwcWtyUVFnYWYzNVI5OFFhd3oyOHRKcXBQdUl6YTQKdURBTFNsaVNacmV0Y0RyNzdKNTdiaEhmdnZvMk9qL0EzdjV4cWVBdjVCYW9YV0FRZmc1YUxXYUNhVUFPaEpHUApkYmsrcEphenN4aFNhbHpWc1p2dGlrV0Q5Zm9jZXgwSkZadGoyQytReTVpNlY1VnpWaFFVTG5OMXZLTVhxUmZCCmhnQzdyZ3RnYUpHV0hnbVJ6RUJGOHkxRUVFMWZvaGJvMnNxa0c0b016M2pCWjRvNE1BRFFjcGZLMnFjaGdybmsKT3hJUy91VThzemR1ODRpSDhzNkYvSGwxKzg3am5xNk85UmUwaU1TdXZ5VWJqQUVlOENtOVAvYTVNMVg5ZXl6dwpXU1hTUFpCd0tTUm9QM3d1eWNiRW9uVFdRblFIZHd5U1krSXZkdGdsaUVEaEtyVmJaR25rczV6bWFhSXlkVy95CkxTMlM5SlJNNVkrWHAwdlYzbkdsRWVoQ1VkclhvUTFEei9BaUhuV0hqYnhvQ0ZHdDBxTDZDT0p6aUFHZlVYS2EKY1E1aURkN3pjMkozbTJaNmM4Vzh4a1BKZSsxZG1OV2ZHSHJqYThEU0h0VGNEWTZBcWQ5OFZ1MG5pdThQQzdieApBdncrKzZKMndHN0xOODlyZ1IwdVA3YXM5Q3g0a0hIc09Gd3ArbEtPRFZlMmR3MHZBUUtDQVFFQTdtb05DalA2CjVQa1NrU05QaS9qdzFZL0ZDd0JvSkV6bDNRNWZ0d0dya1laRlJMQlR2Q0NsaTJqaGV4YUMwRDkreWpzVmFMLzIKVmFwNDMveGk1NWlwbmlxdjhjMVdBYzV4RmgrNmhDeWRTNks5b3dEeGxITjc1TUdMcm1yWWpZKzNhTWRvMTVEbQp4NWJ6bk9MTHlNVVc0QWsrNzdNVHcxMmZhZC83TDBBTlh1bUZGajZ5ZGNTOFBIbWhKbG16NVZlZ1d6NWIxS0dRCksvL3BoY3VPbTM0OXhla3Q3SjVrS1JiREVxTE9sWnYvRUlBZENCUU00VTNkNlAvMnZVVXk1bktZRzBGMXhlYUMKbGVWcHIxRVBvRUkrWGtUeStqam9hQnM3aVVIcGNEMzU5WFFDV0xuaXdmMVlmdHRrOXpKcDdtNnRSL0dlYWJsawp1bm5INXp5Rmt3emxRd0tDQVFFQTdhRnROc2pMMFVFWGx5QllqQ1JPb1B1NmthL280UXlFYVd2TUhjaVh1K0d2Ck03VFFDRjJpOW9lUVhBQkl5VE5vUXIrcE5qQVJib1k4cDArOVpmVjhRR2x2SDZhd1cyTU56RDA3bGc5aHdzalkKSk9DSTY0WHhaajE4M0doSGdOOS9jRTRQWEJyUUNxUExQQ0tkVjY2eUFSOVdObTlWYTNZOVhmL1J2Y29MaU5CMQpGQWc1YmhiTlFNblIzOG5QSnM5K3N1U3FZQjh4QURLdndtS0Vkb255K1dJTS9HUXlZWmlEbFhFajhFZldRb3VNCndBb2s2VnVoczZjdUxpSEh6WEZSNFk2UkNXUmIybmYyVnJ6V29wejJCcDAySWVIWTBVWnNaZUtucWhhOWR0VXUKWkNJdDJNWlVFTHhpaDlKUyt3ekNYOEJKazN4ZWRpODl6T1pLUng0TWdRS0NBUUVBeHFuVUo5WmNrSVFEdHJFbgp6Y2tvVmF5eFVwT0tOQVZuM1NYbkdBWHFReDhSaFVVdzRTaUxDWG5odWNGdVM3MDlGNkxZR2lzclJ3TUFLaFNUCkRjMG1PY2YwU0pjRHZnbWFMZ2RPVW1raXdTM2d1MzFEMEtIU2NUSGVCUDYvYUdhRFBHbzlzTExydXhETCtzVDUKYmxqYzBONmpkUFZSMkkraEVJWTFOcEEzRkFtZWZvVE1ERnBkU0Q5Snl6MGdMRkV5TEJYd1MyUTlVSXkwdUdxQQpjSTFuU0EwZjJYVzZuSXA5RG9CZmlFY3U2VDczOGcxVEZrTGVVUk5KTlRuK1NnemZOb2I3Ym1iQUZjdk9udW43CkRWMWx2d1BSUERSRFpNeWNkYWxZcmREWEFuTWlxWEJyeFo0b0tiMERpd0NWU0xzczVUQXZBb1licTA5akJncG0KZTd4WkpRS0NBUUVBM2Y3bDBiMXFzNVdVM1VtSmozckh2aHNOWTljcnZ6cjdaS1VoTGwzY2F0aGUzZlk0TnVpTApPcmJReFRJNnpVUnFUWmxTRWw1N21uNXJvWDZjR09scVo1NVlBd0N0VnVMRjNCMEVVcDhTSEcrWGhYUUNWYzF2CkJLM0N2UUhxY3RuWTYyanhib0ZhQSthYkVoWGdXaTdJK3NWMHZDdnNhQlV4SldTOVpBbWlGdkZ2dndRajZ0WUEKY0Z0YTV5OVlpQkJtYytldHgxaThaVXYwNktzeXhxNy9QNzA3Rm5yZ21rNXA5eTJZZm53T0RXTGpYZkRjSk9uRwp1ZGdnQzFiaG11c1hySm1NbzNLUFlSeWJGTk1ielJUSHZzd1Y2emRiWDc3anU1Y3dQWFU3RVEzOVpleU1XaXlHCkVwQjdtQm1FRGljUVczVi9CdnEwSU1MbmdFbFA4UHFBZ1FLQ0FRRUFxNEJFMVBGTjZoUU9xZTBtY084ZzltcXUKenhsMk1NMEtiMkFCRThmeFEydzRGeTdnNDJOb3pEVVcxMy9NTjdxMUkrQXdNaGJsNEliMlFJbUVNVHVGYUhQWQpBM09abG5FOUwwb2k0Rkkra0cyZUpPQi8rNXBIU3VmL2pyWi80Z0FSSyt1Yy9DRGVhSWxqUC9ueHcwY1grc0YrCkhqWDRPYjQvQ3lFSWVJVUdkT0dzN2c5a2Yrb2lyWHJ5dURjWnhsLzJmUU94cXZhOWRoaEJMaFBYRzNvdFNwMFQKRDkweEMxbFNQTElIZitWVWlGOWJMTXRVcDRtZUdjZ3dwWFBWalJWNWNibExyUDlQeGJldmxoRzJEM3ZuT0s5QQo4aldJOVAxdU5CRUFVVFNtWFY4cmVNWU95TlhKSDhZYmJUNHlpYXJXbmFRTTBKMGlwV3dYR0VlV2Fndi9hQT09Ci0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg==",
					config.StakingCertContentKey:      "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZOekNDQXg4Q0NRQzY4N1hGeHREUlNqQU5CZ2txaGtpRzl3MEJBUXNGQURCL01Rc3dDUVlEVlFRR0V3SlYKVXpFTE1Ba0dBMVVFQ0F3Q1Rsa3hEekFOQmdOVkJBY01Ca2wwYUdGallURVFNQTRHQTFVRUNnd0hRWFpoYkdGaQpjekVPTUF3R0ExVUVDd3dGUjJWamEyOHhEREFLQmdOVkJBTU1BMkYyWVRFaU1DQUdDU3FHU0liM0RRRUpBUllUCmMzUmxjR2hsYmtCaGRtRnNZV0p6TG05eVp6QWdGdzB4T1RBM01ESXhOakV5TVRsYUdBOHpNREU1TURjeE1ERTIKTVRJeE9Wb3dPakVMTUFrR0ExVUVCaE1DVlZNeEN6QUpCZ05WQkFnTUFrNVpNUkF3RGdZRFZRUUtEQWRCZG1GcwpZV0p6TVF3d0NnWURWUVFEREFOaGRtRXdnZ0lpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElDRHdBd2dnSUtBb0lDCkFRRGRUb1I2MG5hNk51UjlpU0FVTXl6UFhKTk1XVlFiTHlUNS9pWkNpSjNCQjRZV01CaGZ4cEpXSmlXWGNNK3oKbkRncEp1eUNFZWg1RHA2WlkzRmU3azZIaHQ2Rm1GcERqd25qcFFtZGtFS1VnMDBHK0VsUFRwL1VzbXNQTCtKQQpzd1BxQlpXcE1CUzNkc1hRTnVuTU10TUdscmY1UzBsNlhYNHk3a2MvR1R4WWd2ZVdaOUp0Ui9tMktOZXIrd2pnCkJIcUo0clBxbkhCMzBzRFlQWmc5MUN6MUFrOEJiMncySTEwOHpRVmdLSzZlSXFOS1hKSi80cGl6U1pkVTQ5MjAKd014WUJwbmZEQWNobnhlaTlVL3YzUWJUN2VLVUkyZkdyK2hPV1RJV1U4MCtWZU9CdDhhNlA0c1M5QVFoNS82Rwo4cXdtQXFPM1lROWR4TjgyaXUvSDMrTitHR2EvTTByNXJFV3J6d0l1Rmh3S3Z5UWNwUFJCbTJ5UW5CbmhMOUc1CmtONm40T0JNMEtzZ1ozQ1lsSFpTZzRlV2NOZ0J0MVdDRnNRYzd2ZlVGYUpucjhRUDNwRjRWLzRCb2s3d1RPNUgKTjBBMUVZRVZZdVg1M05HbnJLVmUrRmc5K3hNT2dYUFdrVU5xZHZwSTlaYlYzWjBTNTg2NnFGMy92QlpyaGdDcgpLYzVFL3ZNZXhCUmU4S2k0d0txT05WaGk5V0dVY1JIdkZFaWtjKzdWclBqMFlhRzZ6VkxkK3VPQUpOODFmS09QCllvNFg0c1pyTXlQWWwzT2pHdE1oZlY0S3ZDYUxFcjFkdU9rbHFPNmNDdkdROGlBbExWeTNWSnlXNUdKMEQwS3kKaUFpcjRWTmRBSktvMVpnaUdpdkpMV3VsVGZqVWlmQ045bzExNUFpcUp4aXF3d0lEQVFBQk1BMEdDU3FHU0liMwpEUUVCQ3dVQUE0SUNBUUNRT2R3RDdlUkl4QnZiUUhVYyttMFRSekVhMTdCQ2ZjazFZMld3TjNUWlhER1NrUFZFCjB1dWpBOFNMM3FpOC9DVExHUnFJOVUzZ1JaSmYrdEpQQkYvUDAyMVBFbXlhRlRTNGh0eGNEeFR4dVp2MmpDbzkKK1hoVUV5dlJXaXRUbW95MWVzcTNta290VlFIZVRtUXZ3Q3NRSkFoY3RWQS9oUmRKd21NUHMxQjhReE9VSTZCcQpTT0JIYTlDc1hJelZPRnY4RnFFOTFQWkEybnMzMHNLUVlycm5iSDk5YXBmRjVXZ2xMVW95UHd4ZjJlM0FBQ2g3CmJlRWRrNDVpdnZLd2k1Sms4bnI4NUtESFlQbHFrcjBiZDlFaGw4eHBsYU5CZE1QZVJ1ZnFCRGx6dGpjTEozd28KbW5ydDk1Z1FNZVNvTEhZM1VOc0lSamJqNDN6SW11N3E5di9ERDlwcFFwdTI2YVJEUm1CTmdMWkE5R001WG5iWgpSRmkzVnhMeXFhc0djU3phSHd6NWM3dk9CT2tPZGxxY1F6SVNSdldEeGlOMUhrQUwraGtpUUN1TWNoZ09SQWdNCnd6UG9vYThyZld0TElwT1hNcHd1VkdiLzhyR05MRVBvdm9DSzl6NmMrV1oremtSbzQrM1RRa09NWTY2WGh0N3IKQWhseTNsZXIrVHlnNmE1alhUOTJXS0MvTVhCWUF5MlpRTm95MjA0a05LZXZjSDdSMmNTa3hJVGQzbjVFYWNOeQo1TUF0Q05JazdKd2VMQ2g5ckxyTFVCdCtpNG40NHNQK0xWaGZXSGVtbmdBOENvRjRuNmVRMHBwMGl4WlRlbjBqCjR1TjBHMk5mK0plR01scW9PYkxXZElPZEgvcGJEcHBYR29aYUtLRGQ3K2JBNzRGbGU1VWg3KzFlM0E9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
					config.StakingSignerKeyContentKey: "QXZhbGFuY2hlTG9jYWxOZXR3b3JrVmFsaWRhdG9yMDI=",
				},
			},
			{
				Flags: FlagsMap{
					config.StakingTLSKeyContentKey:    "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKSndJQkFBS0NBZ0VBdkpsUTA2QjI1RkJkb0VYVlg2Y25XU3pTbmtOL0hlaDJSWkZETjFOWFZpMlJVbVRiCjJvRGZJWlVMMlA3VUx0d09OS2RmVm9DbTgxbWEzdDdZTlFKTVBDYzdYYy9ZSmVFVEo5ZU5pQ0R0d29zQ0tiWkIKTkNhS1cyWVpodVh3QXRwL3pGMExYNG5pTjRjM2tkT0MxdUNZQ3lzMzl6Wi9NV1haUVlDWHdaaSthbEZKeE96dQp5R3lSaHBtdW9FZzkzc0hyUk9pa0diVGJYOU1VU0w5UEhhR1NtTUU1ZWlMWkhSaXUrcm42cXRQZkJsY0Zyd05jCkVycDN1UGpCWUxQVGVIaFNZK2ljcmZPWDRpdjNNRDhleWtYWUdJQWM2MlVPQkQ3SVVaOEZxd1U0bmpqbWlid0EKTmtUTVRCR015a2F5UUFMR0NOYzhiVGNxaUZHZ1UzTVptcy9qVjFCbHZzQjhDRTRuTkJxZjEwSnRpOEsvNWNTRgplanB3SlM5d29HMGw0cTBwNkJ2bmgwUHJ3OHdRSEVRNUV3SUwveGxDTTVYNms5a3JVeXROcmlBWUNXTTd4VXh1CktaUjRyWEhFWDh2TU1iSWVXZk1SdmZaWmowRUxTN0o1VUFGZmk0dE9nbS9BaCtJVkZhSWxvbGJPVUFHb2FscHIKK1g2YUY5QkxOdWtjbEIxeG00eE50TnFjakRBTzQxSHExRlNqSW96cUJtYlhCK3N3ZUtmM1NkRTdVNkVjRG9LdwpPelFpUFFHRmJhbFY2MXh2N25ZL0xxOXE5VzAxY0NHeDdEbmt4RDk0R05KaDBWcHRoUDJ5NDdmYjIyQ0VWTVVmCk1WcHk1RzNnYzRqVThJUHQrbzNReGZ1b0x0M0ZNLzUyMHAvR0dQbU5ZNDE3ak41YWhmSWpHZ0tHNGJzQ0F3RUEKQVFLQ0FnQSt1SElUM3lLSzdWc2xxUE83K3Rmd0pTTHFOU0k2TFF2Z09OMzBzVWV6UmpZMUE0dkdEK09reEcrTApPN3dPMVduNEFzMkc5QVFSbS9RUU9HWUl3dm5kYTJLbjRTNU44cHN2UGRVNHQxSzZ4d1h5SDBWeDlYcy95Q1duCklpTCtuL0d1WWljZEg3cldvcVpOWGR6K1h2VFJpZzd6clBFQjJaQTE0M0VVbGhxRk93RmdkemMxK2owdldUNmsKMlVHU0trVjJ4ak9FeFF2THcyUFVpYUxqQk0rKzgwdU5IYmU4b0cvWXZDN3J6c2cxMEl6NFZoS3h1OGVEQVY4MgpMTGVnTWN1Y3BFZ3U1WHJXWWE2MElkbTRoUi9Iamh1UUFTeDNKdlh4aHdRWWl3VDRRWTRSc2k4VDNTOWdBTm9rCmp2eEtvMkYrb1MzY1dHTlJzR3UwTk93SCt5anNWeU1ZYXpjTE9VZXNBQWU4NXR0WGdZcjAyK1ovdU1ueHF0T0YKZ2pJSFkzWDVRWmJENGw0Z2J3eCtQTGJqc2o0S0M2cjN5WnJyNTFQZExVckJ2b3FCaHF3dUNrc2RhTW50V0dNRQp1MFYvb29KaTQrdXpDWXpOMDZqRmZBRlhhMnBXelZCNXlLdzFkNnlZaTlVL2JQZDR4bjFwaExVTUhyQzJidmRNCkg4UDE4Z0FTNnJrV24rYWdlaVdSSG1rZjR1b0tndjNQck1qaWprQmFHcGY2eGp2NiswUTM5M2pkVklDN3dnSlYKOFcwaTFmMUF3djY4MDg5bUhCRWFyUFR2M2d6MzkyNTFXRkNQTlFoRXVTeTc5TGk1emp3T3ByWlhTME1uSlhibQpCMDBJUFRJdzUxS3Vhb3VlV3pZMXBBMklGUS8wc0gzZm8ySmhEMHBwMWdJMERkZTdFUUtDQVFFQTdSVmdOZWxrCjNIM1pKc29PZk9URmEwM2xuWHVFZlRBeXhoRUVDUno2NGs1NHBTYkVXVjczUEllWUJ5WnZuc3JLUnlkcFlXVVAKQ3AvbUtoQUpINFVDZjJoelkwR3lPNy9ENitIRUtaZENnNmEwRE5LY2tBb0ZrQmZlT2xMSkxqTFZBVzJlRVZ4egp0bEZ0Ky9XQkU5MEdDdkU1b3ZYdUVoWEdhUHhDUHA1Z2lJTjVwaFN6U0Q1NTdid3dPeVB3TktGWjdBbzc3VU5LCmt6NkV6Y3ZRZ3FiMjA1U1JSS0dwUzgvVC85TGNMc1VZVmtCZllRL0JheWpmZk8rY1FGNHZINXJCNHgvOC9UN3QKdVVhNzl1WStMZUdIZ1RTRklBdWk5TEVLNXJ5Ly8yaERKSU5zSXRZTWtzMVFvNFN1dTIzcE91R2VyamlGVEtXbAptT0lvRm1QbWJlYkFjd0tDQVFFQXk2V2FKY3pQY0tRUS9ocWdsUXR4VTZWZlA0OVpqVUtrb2krT0NtdHZLRHRzCjdwSmZJa3VsdWhuWUdlcUZmam5vcHc1YmdaSE5GbTZHY2NjNUN3QnRONldrMUtubk9nRElnM2tZQ3JmanRLeS8KQlNTVjNrTEVCdmhlOUVKQTU2bUZnUDdSdWZNYkhUWGhYUEdMa2dFN0pCWmoyRUt4cDFxR1lZVlplc1RNRndETQpLRUh3eklHY0ZreVpzZDJqcHR5TFlxY2ZES3pUSG1GR2N3MW1kdExXQVVkcHYzeHJTM0d2ckNiVU1xSW9kalJkCnFrcmcvZC9rUXBLN0Ezb0xPV2ZhNmVCUTJCWHFhV0IxeDEzYnpKMldsc2h4SkFaMXAxb3pLaWk1QlE5cnZ3V28KbXVJNXZkN282QTlYc2w4UXpsdVNTU1BpK05oalo2NGdNQnJYY2lSdm1RS0NBUUIvZEI1azNUUDcxU3dJVGxlNwpqTUVWRHF1Q0hnVDd5QTJEcldJZUJCWmIweFBJdFM2WlhSUk0xaGhFdjhVQitNTUZ2WXBKY2FyRWEzR3c2eTM4ClkrVVQyWE11eVFLb1hFOVhYK2UwOUR3dHlsREJFL2hXOXd4R2lvNU5qSFBiQWpqQXE4MXVSK1ZzL2huQ2Voa0sKTktncStjT2lkOU9rcFZBazRIZzhjYWd6dTNxS2JsWnpZQ0xzUzE4aWJBK1dPNmU3M1VTYUtMTE90YTF2ZFVLQworbjkyLzBlWlBjOWxralRHTXZWcnIwbUdGTlV4dU9haVZUYlFVNEFNbXBWNnlCZXpvbDYvUmpWR2hXQkhPei95CktteE9hWTJuekptdU1mOUtTKzVyd0FGWWY4NkNhOUFXbTRuZVhsWVJMT1ZWWWpXTU01WjF2aGRvT1N5VDNPRGoKOUVsQkFvSUJBR0NSUGFCeEYyajlrOFU3RVN5OENWZzEwZzNNeHhWU0pjbDJyVzlKZEtOcVVvUnF5a3Z6L1RsYgphZnNZRjRjOHBKTWJIczg1T1R4SzJ0djNNWmlDOGtkeDk5Q1VaTDQvZ3RXOVJXWkh2dVY5Q1BQQ1hvTFB2QzdsCjlmanp0ZDFrcUpiN3ZxM2psdGJxSnR5dytaTVpuRmJIZXo4Z21TZVhxS056M1hOM0FLUmp6MnZEb1JFSTRPQSsKSUorVVR6Y2YyOFRESk5rWTF0L1FGdDBWM0tHNTVwc2lwd1dUVlRtb1JqcG5DemFiYUg1czVJR05FbFd3cG9mZgpGbWxXcFIzcW5vZEt4R3RETVM0WS9LQzJaRFVLQVUrczZ1Ry9ZbWtpUDZMZFBxY2tvZDRxSzhLT1JmMUFSOGRMCkJ6WGhHSklTSURNb25rZU1MTThNWmQwSnpXSWwzdmtDZ2dFQVBCa0V4ZDJqNFZZNXMrd1FKZGlNdG81RERvY2kKa0FFSXZJa0pZOUkrUHQybHBpblFLQWNBQVhidnVlYUprSnBxMzFmNlk2NnVvazhRbkQwOWJJUUNBQmpqbEl2ZQpvN3FRK0g4L2lxSFFYMW5iSER6SW5hRGRhZDNqWXRrV1VIakhQYUtnMi9rdHlOa0Z0bFNIc2t2dkNFVnc1YWp1CjgwUTN0UnBRRzlQZTRaUmpLRXpOSXBNWGZRa3NGSDBLd2p3QVZLd1lKTHFaeHRORVlvazRkcGVmU0lzbkgvclgKcHdLL3B5QnJGcXhVNlBVUlVMVUp1THFSbGFJUlhBVTMxUm1Kc1ZzMkpibUk3Q2J0ajJUbXFBT3hzTHNpNVVlSgpjWnhjVEF1WUNOWU11ODhrdEh1bDhZSmRCRjNyUUtVT25zZ1cxY3g3SDZMR2J1UFpUcGc4U2J5bHR3PT0KLS0tLS1FTkQgUlNBIFBSSVZBVEUgS0VZLS0tLS0K",
					config.StakingCertContentKey:      "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZOekNDQXg4Q0NRQzY4N1hGeHREUlNqQU5CZ2txaGtpRzl3MEJBUXNGQURCL01Rc3dDUVlEVlFRR0V3SlYKVXpFTE1Ba0dBMVVFQ0F3Q1Rsa3hEekFOQmdOVkJBY01Ca2wwYUdGallURVFNQTRHQTFVRUNnd0hRWFpoYkdGaQpjekVPTUF3R0ExVUVDd3dGUjJWamEyOHhEREFLQmdOVkJBTU1BMkYyWVRFaU1DQUdDU3FHU0liM0RRRUpBUllUCmMzUmxjR2hsYmtCaGRtRnNZV0p6TG05eVp6QWdGdzB4T1RBM01ESXhOakV5TWpKYUdBOHpNREU1TURjeE1ERTIKTVRJeU1sb3dPakVMTUFrR0ExVUVCaE1DVlZNeEN6QUpCZ05WQkFnTUFrNVpNUkF3RGdZRFZRUUtEQWRCZG1GcwpZV0p6TVF3d0NnWURWUVFEREFOaGRtRXdnZ0lpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElDRHdBd2dnSUtBb0lDCkFRQzhtVkRUb0hia1VGMmdSZFZmcHlkWkxOS2VRMzhkNkhaRmtVTTNVMWRXTFpGU1pOdmFnTjhobFF2WS90UXUKM0E0MHAxOVdnS2J6V1pyZTN0ZzFBa3c4Snp0ZHo5Z2w0Uk1uMTQySUlPM0Npd0lwdGtFMEpvcGJaaG1HNWZBQwoybi9NWFF0ZmllSTNoemVSMDRMVzRKZ0xLemYzTm44eFpkbEJnSmZCbUw1cVVVbkU3TzdJYkpHR21hNmdTRDNlCndldEU2S1FadE50ZjB4Ukl2MDhkb1pLWXdUbDZJdGtkR0s3NnVmcXEwOThHVndXdkExd1N1bmU0K01GZ3M5TjQKZUZKajZKeXQ4NWZpSy9jd1B4N0tSZGdZZ0J6clpRNEVQc2hSbndXckJUaWVPT2FKdkFBMlJNeE1FWXpLUnJKQQpBc1lJMXp4dE55cUlVYUJUY3htYXorTlhVR1crd0h3SVRpYzBHcC9YUW0yTHdyL2x4SVY2T25BbEwzQ2diU1hpCnJTbm9HK2VIUSt2RHpCQWNSRGtUQWd2L0dVSXpsZnFUMlN0VEswMnVJQmdKWXp2RlRHNHBsSGl0Y2NSZnk4d3gKc2g1Wjh4Rzk5bG1QUVF0THNubFFBVitMaTA2Q2I4Q0g0aFVWb2lXaVZzNVFBYWhxV212NWZwb1gwRXMyNlJ5VQpIWEdiakUyMDJweU1NQTdqVWVyVVZLTWlqT29HWnRjSDZ6QjRwL2RKMFR0VG9Sd09nckE3TkNJOUFZVnRxVlhyClhHL3Vkajh1cjJyMWJUVndJYkhzT2VURVAzZ1kwbUhSV20yRS9iTGp0OXZiWUlSVXhSOHhXbkxrYmVCemlOVHcKZyszNmpkREYrNmd1M2NVei9uYlNuOFlZK1kxampYdU0zbHFGOGlNYUFvYmh1d0lEQVFBQk1BMEdDU3FHU0liMwpEUUVCQ3dVQUE0SUNBUUFlMmtDMEhqS1pVK2RsblUyUmxmQnBCNFFnenpyRkU1TjlBOEYxTWxFNHZWM0F6Q2cxClJWZEhQdm5pWHpkTmhEaWlmbEswbC9jbnJGdjJYMVR6WU1yckE2NzcvdXNIZjJCdzB4am0vaXBIT3Q1Vis0VE4KbVpBSUE0SVBsMDlnUDI4SVpMYzl4U3VxNEZvSGVNOE9UeGh0dE9sSU5ocXBHOVA1ZDZiUGV6VzZaekkzQ2RQUApDRjY5eEs0R0Zsai9OUW5Bb0ZvZ2lkNG9qWVlOVGovY000UFlRVTJLYnJsekx5UHVVay9DZ3dlZlhMTUg4Ny9ICmUza1BEZXY4MFRqdjJQbTVuRDkzN2ZaZmdyRW95b2xLeGlSVmNmWlZNeFI3cWhQaGl6anVlRDBEQWtmUUlzN0wKWVZTeXgvcWpFdjJiQllhaW01UlFha1VlSFIxWHU1WGovazV6cjMzdDk3OWVkZTUwYnlRcmNXbTRINUp4bkVwRApKeEpuRmZET1U2bzE0U0tHSFNyYW81WjRDM2RJNTVETTg0V0xBU25sTUk1Qks0WHRTM25vdExOekc4ZGZXV2hUCjltMEhjcnkrd1BORGNHcjhNdGoxbG9zLzBiTURxTUhDNGpjRlcxaHJYQ1VVczlSWXpFK04veG9xd0NRU2dOMVAKRTczdVhUeVNXajVvdk1SNVRQRjZQaGNmdExCL096aXFPN0Z2ZXJFQnB2R0dIVUFuVVQ2MUp0am9kalhQYkVkagowVmd5TU9CWTJ5NTNIVFhueDNkeGVGWmtVZFJYL1ZaWXk4dE1LM01UWSs3VUlVNWNXWW5DWkFvNUxOY2MwdWtSClM2V1M5KzZlYVE2WFJqaGZOVWp4OWE3RnpxYXBXZHRUZWRwaXBtQlAxTmphcDNnMjlpVXVWbkxRZWc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
					config.StakingSignerKeyContentKey: "QXZhbGFuY2hlTG9jYWxOZXR3b3JrVmFsaWRhdG9yMDM=",
				},
			},
			{
				Flags: FlagsMap{
					config.StakingTLSKeyContentKey:    "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKS1FJQkFBS0NBZ0VBMlp3NkF4eE5wNC9Oc1E0eDlFMit6bHpLa0F4OC9zMnk0bmJlakVpTlZ3ZDd6Z0NCCklqNnE3WHB4cDZrVzFSWEp1Um1jYjhZZUdFQS9Bb3lrcE5hb0dTWE03TkF6MG9oa1BCSDVlRHhqdU1aeEc1WlkKQzl6MUVUMXFlNWhGZDZJZW44U0FvRUNrd1pVK3U5Ukp4Y3dlWmNpVnFicGtjN3dOYjhvUVRMR1BHanZzWGF1UwoxQlpiV013WGI2WUMxMVdnOEIyVU5qMEJGclJkSGRDVHRqZEZoUlF4alpzZXAvS1NrbmlBRlE3RThHUW1TdmhRCm5uaE1xQm4xN0NEYkhOTWdqRUxKV3NKSHpqS2dQY2dWZHlZdWtKaFdDc2htTGV1VEZoamFqd09GNnlNcFlpc0EKZ2w4TS9abzJsc2plNndNc1BYRnBTekRnelpnemtWc3hrV2VkdmREQkFCQ0diS28yQjFZclVVelAwWnpTR25VNgpHUmp0TXViZ0pETmVISms3NTdDdFFqL2Rxclgwb3JLdStWdnlQZFhhMDRhQkxVeXUvRkJqcGYrVU5XQVVkUEJmCnJVbEZtVnNoM2lPbERnVkFoZ3Rtd3Jac3lqSmU3MU51WlJ1cG13a2RTYmtJdWtXS1ozekkxeDkrcFZlRXUzMDcKMHNNTGtkTjRkaGR4OVhoWkZqUnRwV3hhMXRIY3d3ajRONVY4SmhVT3VZVkhFSVhMYzU2QWMrUXRaUmRubHROOApMbFlNWXA1N2xaU1I0OHZ4cTZwbHJBRXA5VHc3bGJPSk81T2d5WDhNaHdJZ05Mc1Y2eG44V0w2ZXgvSStQdG1ECnBsb2xwZWNXSUt4QTQ3cStaL0djZU9qeDBIcTlnTDZ0N0d4QmhIdFRBclZ5Wlk5M2EzZVI0cHQ1dmpVQ0F3RUEKQVFLQ0FnQk1vQk5aWnd6OUZNa0VNSkJzaXpmRjZLeTNQbjZCSnFOMzFRMldiakcrMUhiRzJpeWVoMXllMUwvUwpudHJZVzV5MW5nd1UyN2xiSnJ4SlJJYnhPRmpteWdXMzJiUjF6T3NtcjltZGVmNVBZU2tRNHNiTUhwajQ0aHh0CnV2ZXpJWllSQWh1YzBrWnhtQUVJR0wrRmM5TzhXWDVCenMxeVoyUi8yYklWbjJ4WmU0SkdsWlRWTTY0a3ZYRC8KTW9ETG5HNVlQc0lpdXlaMy9UalF0OUpibG1qWGJIM3FkQlcrWTg4eTNsV1RsS2pLVVNtZXVvT0EyYkY4ZSsrNQpudlFvMlRzYnlLU29YY0wxRzZTTFBMbzZRMnFnSmRRZVplUjlCUGU5RHpGZXJJbnFlMjRtRUNoVXYrMk9HMUJmCmxnblF6VVExdW9xdUhGNzhaank2VVZkSjhTZDh1ZnZLQzlyejhKWXNJeW5mdzBnUUMzRjgvZW1tMVFTYWJGdlkKdEc0K3gwSzhGZ3JpampFMDhSdnFnSW5keDlmdENOb040dTNsWHhQckpoS3ByMnh1WFNhNFZaYnVtZ043ZnFXeApVQkM4bG1QUWk1VlptajNuSmZqNGRhdG1CVHZzMWRPTFJNZGZkdFRGeitjQWRXTlp4WDNIT0xaVVNxTVZXZ1hZCmtYMHM3SVY5R255VW50Qmt0WCtJRWJXbEF0dHpsZHlxRjltZDRhdmpLWFErWTRQSy9zUjF5V3N1dnRpWmRZVUwKL1FyUUhYMENzVnYxaFJjWDB5ZWtBMGE4cXdhR214RWNuZEVLdjd3RjFpNjI2amMyZkRSNnFJMXlwMjBYbDNTaQprWUJTTmg3VksyMTBYSWhkZFN1VnhXNS9neU5uRkFCRGZwMWJTZFRoNVpKUmZOdnRRUUtDQVFFQTlaaXBueXU4CkpLbEx0V3JoMmlQOXBtRkJhZVVnb0Y2OElWZmQ2SVhkVjduQUhTV3lzaUM0M1NDVW1JNjN4bHVNWUVKRmRBWisKbS9pUmMvbjZWRktFQlc0K1VqazlWVzFFMWlxSGdTQUJnN250RXNCMk1EY1lZMHFLc040Q1lqQzJmTllPOTd6Sgo1b2p1ODRVM1FuOFRXTmtNc3JVVTdjcm0yb0FRZDA4QWl6VkZxTG8xZDhhSXpScSt0bDk1MlMvbGhmWEtjL1A5CmtmaGwrUktqaVlDMnpiV25HaW54YzJOYmY1cFd3bm10U3JjZW5nK1prZ1ZmU0IzSHZTY2txekVOeWU5WWtwVk0KR0UrS2pFZHNzK1FuR1FSV00ySlBseW9ZRG1oVDZycmFzUlQ2VEtzZWN3bzFyUlhCaTRDMWVUWlFTblpmMjRPZwpRdXJTLy9Yekh6Ym5rUUtDQVFFQTR0UVNtYUpQWk5XWXhPSG5saXZ6c2VsZkpZeWc1SnlKVk1RV3cvN0N2VGNWCkdPcG9ENGhDMjVwdUFuaVQxVS9SeWFZYVVGWitJcDJGaEsyUWNlK3Vza05ndHFGTjlwaGgvRGVPMWc4Q1NhSWUKNkVidGc4TjhHTGMwWWhkaUp0ZDJYR3JrdGoyWHRoTUw3T0pQWUlpZGQ0OHRHdVFpemZpam80RmUxUzByU1c1NgpCNFJIVGgvTzZhMHRhTmVGYm5aUUpENTJoYTl3bG5jL1BaU0NVTWI5QzBkMDhkU3hkQlFWK1NWZEdybC9JUmZDCnFISG9DODZHWURjbW52aUQ1Q0ZPeHB4N0FKL2hRQXdQRlFSQ25XR0h3RGpwY29NT3RrdHlvN3BqOU1EdXpCVWIKa3I0cjFlaThmN1BDOWRtU1ltWXpKTVF4TGZ6K1RpMlN5eU9tZE0xQ1pRS0NBUUVBc1ZyNGl6Q0xJcko3TU55cAprdDFRekRrSmd3NXEvRVROZVFxNS9yUEUveGZ0eTE2dzUvL0hZRENwL20xNSt5MmJkdHdFeWQveXlIRzlvRklTClc1aG5MSURMVXBkeFdtS1pSa3ZhSlA1VythaG5zcFg0QTZPVjRnWXZsOEFMV3Bzdy9YK2J1WDNGRTgwcE9nU20KdmtlRVVqSVVBRzNTV2xLZldZVUgzeERYSkxCb3lJc0lGNkh3b3FWQXVmVEN5bnZUTldVbE9ZMG1QYVp6QldaWApZUEhwa1M0d0tTM0c1bndHMUdSQmFSbHpjalJCVVFXVThpVWRCTGcweUwwZXR0MnF4bndvcTFwVFpHNzBiNDhZCnllUGw5Q1AwbUJEVHh5Y256aWU3Q2hTNzN3dDJJYTJsUkpCSDZPR0FMbHpaTUZwdnF3WkcvUC9WMk4wNVdJeGwKY05JMmNRS0NBUUVBb3lzN1ZobFVVNHp6b0cyQlVwMjdhRGdnb2JwUDR5UllCZ29vOWtURmdhZW1IWTVCM1NxQQpMY2toYWRXalFzZHdla1pxbDNBZ3ZIWGtIbFZjbXhsMzZmUmVGZ0pqT3dqVE04UWpsQWluOUtBUzY3UmFGM2NBClJpZEVIMndDeHo0bmZzUEdVdkpydUNaclpiUkd0WUtSQS9pUzBjMWEzQ0FJVnc0eFVkaDBVeGFONGVwZUFPMFEKd3pnNGVqclBXVzd5cDUvblVyT3BvaE9XQW81YVVCRlU1bEE0NTkzQTZXZXBodGhCNlgrVzNBOWprQmlnZkIzTQp2Rm53Qmx0dlJTUlFycjdTSE5qbUNGU2taTkh6dVpMM1BHZTBSeFBQK1lLOHJOcmdIS2pOSHpIdjY5ZXhZT2RTCjhlbzJUUFIrUVJxVG45Y2lLWnJjdFJCRGtLM01pQ2svb1FLQ0FRQVpJWmRrT0NsVVBIZlNrNHg1bkJYYXNoS1kKZ0R6ZXlZSFlMd05hQmRFS2dITnVmNmpDbHRLV29EelpzcXJ2MU55YS8xNDhzVGdTVGc5MzFiYmNoK2xuSEtKZApjWHJDUVpXQm51MlVxdWlzRk1lTk92cHAwY1B0NHRJWURaVkNSTVJyd0lsWnFJSnhiMm5Bd0Z2YjBmRWZMays0CmdtdSszY0NhTi92UzNvSkE5RUZremp4RzBYaUxPeW55QVpiNWZZMDRObUZPSXNxM3JnVDREZUN1ckhUS3RPSjIKdDE0b1ROcTA2TEQ1NjZPblQ2cGxMN3ZhTHRUUi85L3FKYzAwN1dqdzhRZGJUdVFBTHFDaldXZzJiN0JWa095UgpvOUdyaFB6U2VUNm5CSEk4RW9KdjBueGVRV05EWDlwWmlXLzFuc3l1QUFGSjlJU2JEV2p6L1R3QjE3VUwKLS0tLS1FTkQgUlNBIFBSSVZBVEUgS0VZLS0tLS0K",
					config.StakingCertContentKey:      "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZOekNDQXg4Q0NRQzY4N1hGeHREUlNqQU5CZ2txaGtpRzl3MEJBUXNGQURCL01Rc3dDUVlEVlFRR0V3SlYKVXpFTE1Ba0dBMVVFQ0F3Q1Rsa3hEekFOQmdOVkJBY01Ca2wwYUdGallURVFNQTRHQTFVRUNnd0hRWFpoYkdGaQpjekVPTUF3R0ExVUVDd3dGUjJWamEyOHhEREFLQmdOVkJBTU1BMkYyWVRFaU1DQUdDU3FHU0liM0RRRUpBUllUCmMzUmxjR2hsYmtCaGRtRnNZV0p6TG05eVp6QWdGdzB4T1RBM01ESXhOakV5TWpWYUdBOHpNREU1TURjeE1ERTIKTVRJeU5Wb3dPakVMTUFrR0ExVUVCaE1DVlZNeEN6QUpCZ05WQkFnTUFrNVpNUkF3RGdZRFZRUUtEQWRCZG1GcwpZV0p6TVF3d0NnWURWUVFEREFOaGRtRXdnZ0lpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElDRHdBd2dnSUtBb0lDCkFRRFpuRG9ESEUybmo4MnhEakgwVGI3T1hNcVFESHoremJMaWR0Nk1TSTFYQjN2T0FJRWlQcXJ0ZW5HbnFSYlYKRmNtNUdaeHZ4aDRZUUQ4Q2pLU2sxcWdaSmN6czBEUFNpR1E4RWZsNFBHTzR4bkVibGxnTDNQVVJQV3A3bUVWMwpvaDZmeElDZ1FLVEJsVDY3MUVuRnpCNWx5SldwdW1SenZBMXZ5aEJNc1k4YU8reGRxNUxVRmx0WXpCZHZwZ0xYClZhRHdIWlEyUFFFV3RGMGQwSk8yTjBXRkZER05teDZuOHBLU2VJQVZEc1R3WkNaSytGQ2VlRXlvR2ZYc0lOc2MKMHlDTVFzbGF3a2ZPTXFBOXlCVjNKaTZRbUZZS3lHWXQ2NU1XR05xUEE0WHJJeWxpS3dDQ1h3ejltamFXeU43cgpBeXc5Y1dsTE1PRE5tRE9SV3pHUlo1MjkwTUVBRUlac3FqWUhWaXRSVE0vUm5OSWFkVG9aR08weTV1QWtNMTRjCm1Udm5zSzFDUDkycXRmU2lzcTc1Vy9JOTFkclRob0V0VEs3OFVHT2wvNVExWUJSMDhGK3RTVVdaV3lIZUk2VU8KQlVDR0MyYkN0bXpLTWw3dlUyNWxHNm1iQ1IxSnVRaTZSWXBuZk1qWEgzNmxWNFM3ZlR2U3d3dVIwM2gyRjNIMQplRmtXTkcybGJGclcwZHpEQ1BnM2xYd21GUTY1aFVjUWhjdHpub0J6NUMxbEYyZVcwM3d1Vmd4aW5udVZsSkhqCnkvR3JxbVdzQVNuMVBEdVZzNGs3azZESmZ3eUhBaUEwdXhYckdmeFl2cDdIOGo0KzJZT21XaVdsNXhZZ3JFRGoKdXI1bjhaeDQ2UEhRZXIyQXZxM3NiRUdFZTFNQ3RYSmxqM2RyZDVIaW0zbStOUUlEQVFBQk1BMEdDU3FHU0liMwpEUUVCQ3dVQUE0SUNBUUE0MGF4MGRBTXJiV2lrYUo1czZramFHa1BrWXV4SE5KYncwNDdEbzBoancrbmNYc3hjClFESG1XY29ISHBnTVFDeDArdnA4eStvS1o0cG5xTmZHU3VPVG83L2wwNW9RVy9OYld3OW1Id1RpTE1lSTE4L3gKQXkrNUxwT2FzdytvbXFXTGJkYmJXcUwwby9SdnRCZEsycmtjSHpUVnpFQ2dHU294VUZmWkQrY2syb2RwSCthUgpzUVZ1ODZBWlZmY2xOMm1qTXlGU3FNSXRxUmNWdzdycXIzWHk2RmNnUlFQeWtVbnBndUNFZ2NjOWM1NGMxbFE5ClpwZGR0NGV6WTdjVGRrODZvaDd5QThRRmNodnRFOVpiNWRKNVZ1OWJkeTlpZzFreXNjUFRtK1NleWhYUmNoVW8KcWw0SC9jekdCVk1IVVk0MXdZMlZGejdIaXRFQ2NUQUlwUzZRdmN4eGdZZXZHTmpaWnh5WnZFQThTWXBMTVp5YgpvbWs0ZW5EVExkL3hLMXlGN1ZGb2RUREV5cTYzSUFtME5UUVpVVnZJRGZKZXV6dU56NTV1eGdkVXEyUkxwYUplCjBidnJ0OU9ieitmNWoyam9uYjJlMEJ1dWN3U2RUeUZYa1VDeE1XK3BpSVVHa3lyZ3VBaGxjSG9oRExFbzJ1Qi8KaVE0Zm9zR3Fxc2w0N2IrVGV6VDVwU1NibGtnVWppd3o2ZURwTTRsUXB4MjJNeHNIVmx4RkhyY0JObTBUZDkydgpGaXhybWxsYW1BWmJFejF0Qi8vMGJpcEthT09adWhBTkpmcmdOOEJDNnYyYWhsNC9TQnV1dDA5YTBBenl4cXBwCnVDc3lUbmZORWQxVzZjNm5vYXEyNHMrN1c3S0tMSWVrdU5uMU51bm5IcUtxcmlFdUgxeGx4eFBqWUE9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
					config.StakingSignerKeyContentKey: "QXZhbGFuY2hlTG9jYWxOZXR3b3JrVmFsaWRhdG9yMDQ=",
				},
			},
			{
				Flags: FlagsMap{
					config.StakingTLSKeyContentKey:    "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKS1FJQkFBS0NBZ0VBNEN1YStiM1I3U1JSSU1PNFJoUDVjeW1oM0w4TTY5OW1xNXF0SlBiV0hJU1YzMDk1Ck5ZOVpWN1FRSFhGRGQ0RWtTbzRNeC83NDI1OUhtdHdJZ3dsSGRqSEpQNTlDbkNXZU4wdVNPMll6KzYyMnI5dmUKbXlnbWRSNis2K1lGMkJZWlB0MmxMRlh3MUtnRHdjTWovQ25sZDlRRVZRUDdQbGozbHFyT3ZVRjAzY2liZ1dCTwphYStOTDlVbi9ubTFDSjJkKzdobU41SWp0Qk1meVlIS0VMQmljNjFnLzg3VzBRaFNMNjNUU1dYZEtScnpKWmt5CnFlMTdERWQzS2I2cE8rOVAvekN3bmNobHY5U0FLMDByZzliM2U1Z1Y5U2NsVnlzbkdEZEx5RHJXNE1UemhiclYKTXFnem5qKzZPZGF1SWowdDNRa1lINXAvNFZpOE12Z3JTWHRBUmlyRytMWk5MU3NhTkxIdkwvU1FQSmxMWjVzNQpqTGVHUFk0b3pxd2pnZTE4RmlwaVdCSnZEZVZHM011bktkcUMvTDBGYmM4OXoxNjVCYmZ0M0JiZFk2NDNhVUdJCkIrSThwSkZnM2pveEg4dFJRc003RFViblMwN3k0WHk1L092MlUrSEMyZlNTTy8xbHRDRHd2QlhPNVljNXlmbXkKRUZIaEozV2huWUZaTWJoSU5ETVhheks5STFOQ0JhRjhra1Z6VWMzQTFtVW9SSXdVNys3WnhjUzVxRFZQcmlXLwp5cVRCK3JFV0l0ODdQT1huR2pDamNodml3NWdXb1ZKUjhlWVNnQitQVHZQT2VmZ0RJYklQZFRqV1NaWFkwRjlLCkdheUNzeXVRYWJLbms1ZUhObUxmM3dDNG1XbmhaTmFRUkxMMVhqRGdRaUxsaFBtK0ZDQkdpd0RvY1g4Q0F3RUEKQVFLQ0FnRUFwdU1QcnhtSDdYbjZBK0J4a1lwUlRWRVROWm50N3JRVVpYRHpzZThwbTNXQmRneGVlbWRMNWlVaApVaW4rUmp1WVh3Qzl0eTYwNmh2OFhPZXVWbzlUNmtSS1JOazE1N1dCd2p5Nmt3b1ZiU3I0TkpnRmM1RkNnREx4CmhBRnRIRi9uVDR3RzZhalpjQmZkSkNVNDV3UHgxM0c1LytqRTVMZXJLem5pUzdjdFgrZDNEYXc2OUNkRGZ2YTcKblpIU0dxWHM5WGRrY2I2VVlmMVN6dHVYS1RHSE9nTTdrWFhWS3kxOHNnNUFuQVgvemhoSUtCZVRSanFNUHFuOQpwdEJRZ1ZRNlJBdGxrVEdkdm1CZlF0MWlwZllsckplZTBUSGhkTEdsbXp1ZmFXT1VrU1ZPL3FJSEVuMXlZRCtsClRtWHFvWWJXWEJYbkpiQUp3Q1FsaC9TRmxXRHlpV1dPeHN6eGR3d1QyeWJ3N09SM2EwREVWME1iS0prVWV4eUYKOTJMcjNxb0JTWlJGUW5YVnZCZ2pRT3duekVGcGgxQU51R1kzb2RMOEpTTTF0SG5pSXNDczRXaERQT3NiQWoraAprd1M1MWNvbE1rM2JOQ1ozeGVBcmpNTEJWTGdUN3hMWC83WlljNy9vVEVGV2lrKzIwVHZTRVd6ZEUxTi80Z2ZKCmpFVS9WcXJuTmp5ZXYydzlBazZiRWt3WkZMUzZWWjlyVFdURjlqazhDMWFYai9SaGZhYUMzM3hYQmJobjlIdVgKbFR1L0phTE1wMFFjNGFDbHFVWU02TGx4SWVqSDViOGZJeENOSEppc2xYSkRhNmE2YVFsODVCaVFPRFBGeFZUNQpXQ3BRRDQ4NThFdUxkWDRCUlcyZklHUlk2RGl2UjZ1SlJBbXhMZitFd0FnL3JnVHpVc0VDZ2dFQkFQU2tIWDVGCkJoUmd1ZEYwTW53Titlbmo0U29YSGhSRytEVG9yeE8xWmgycU45bG5YTzluTUtNQ1hWSkxJVnZHRnVpTVJTSjAKVktmMXUwVXFhQkYwMk1iSXZiZWk3bXpra1cwLzc0bTA0WDM3aXlNbXRubW9vUTBHRVY4NG9PTndBdDNEZWVUZwp2SXBPdHE5VjI2WEhHYVFEeGNSRk1GQnVEMDJhMnlmM0pZa1hqNzRpMnNjTVA0eHhNSE1rSnhHSzlGU0JPaG5wCmsvcDBoTWwzRlZHZm81TnM1VDFSbDNwTXVlRUYzQjUrQnZyVjF6MTRJTi8wbHd1aHVqclVVWVM0RXcrUGs1ekMKRlN1YmZJUU1xU1QxanZYWFRhR2dYMEdQZmZhNGx4Z2FERUFUTGV3dkwzRmp5MjdYemw1N2k5WnZUTkM0eUZhZAo0b2tqci9lSXRIdEtWSEVDZ2dFQkFPcVVLd3cvNnVpSk1OdmMyTnhMVU14dXpCMDdycU9aS1QyVk1Ca0c1R3prCnY4MWZEdGxuZEQ4Y3dIU3FPTEtzY0gvUUtYRDdXSzNGQ3V2WlN2TXdDakVCNFBwMXpnd0pvQmV4dVh2RkREYnMKMFQ3N1Fpd2UrMldtUklpWWV2NWFSRzNsbkJNTThSRFMvUVB6RWRveEhkenJGVVJZVmwwcnY1bC83cndCMlpkNgp4QVlIY1VwWmM0WmF5c0VncVFDdVpRcUM3TXJxN3FmQnlVdGhIMjhZaWN6MTk3OGZwRTNkeDE1Y2VxalU5akJRCnhVVXdiZUtUL1VrUVF2bVlIZHRnd0VqaHpWUUwxT0FBV2tUNlJzc01xeDJSQWRpMFNxV1BGRWh4TlBIQnBHOUIKbEtVREJCSU02ZHU5MTZPbjBCamdoaDNXaHhRS3BUSXp2ZU5BaWV4YlhPOENnZ0VCQU52Sm9oR3ljMzdWVTd3ZwoxOFpxVEEvY3dvc3REOElKN0s2a0tiN2NKeTBabzJsM21xQWZKaXdkVUxoQmRXdmRNUEdtSytxRGR4Y2JCeTloCnBQT2g5YXZKNStCV3lqd2NzYWJrWFJGcjUzWm5DcDcvQmN1Uk8zZlc3cjZNd3NieStEQkNrWDJXaHV6L1FOT1AKb0hGMHljMTM4aktlTW9UZ0RIR2RZYTJyTmhiUGl6MjRWTE9saG1abnZxNkRXWEpDVTdha0R3Mytzd3E5cWhyUwpHTjRuUFMrVEV2VWZHNmN0ellXajNSbXNBaHRUQ1RoWmQ3ZWRLQ0swSHZzQmkyZGdkUWR5NTV4YkplZnlubENJCmkySUFGM3M0L3E3cHhRckNudG1OQjNvSTFONndISDduK1lpMnJxc2J5WFZMSzl2d1RLUHNqMWg2S204cEY4dWQKRHdFQlM1RUNnZ0VBTW5xMkZNbkFiRS94Z3E2d3dCODVBUFVxMlhPWmJqMHNZY016K1g3Qk15bTZtS0JIR3NPbgpnVmxYbFFONGRnS2pwdTJOclhGNU1OUEJPT1dtdWxSeExRQ2hnR1JQZGNtd2VNalhDR3ByNlhubXdXM2lYSXBDClFTcVpmdWVKT0NrR3BydU5iWkFRWkRWekd5RjRpd0tjMFlpSktBNzJidEJXUjlyKzdkaGNFYnZxYVAyN0JHdmgKYjEwa1dwRURyVkRhRDN3REp0dU5oZTR1dWhqcFljZmZCNHM2eUJjd0RVMlhkSmZrRVdiYW42VVIvb1NnY095MQp5YjVGRzE3L3RkREpNQ1hmUUtIWEtta0pBK1R6elFncDNvL3czTWhYYys4cFJ6bU5VaVVBbEt5QkowMVIxK3lOCmVxc010M3dLVFFBci9FbkpBYWdVeW92VjVneGlZY2w3WXdLQ0FRQWRPWWNaeC9sLy9PMEZ0bTZ3cFhHUmpha04KSUhGY1AyaTdtVVc3NkV3bmoxaUZhOVl2N3BnYmJCRDlTMVNNdWV0ZklXY3FTakRpVWF5bW5EZEExOE5WWVVZdgpsaGxVSjZrd2RWdXNlanFmY24rNzVKZjg3QnZXZElWR3JOeFBkQjdaL2xtYld4RnF5WmkwMFI5MFVHQm50YU11CnpnL2lickxnYXR6QTlTS2dvV1htMmJMdDZiYlhlZm1PZ25aWHl3OFFrbzcwWHh0eDVlQlIxQkRBUWpEaXM4MW4KTGc5NnNKM0xPbjdTWEhmeEozQnRYc2hUSkFvQkZ4NkVwbXVsZ05vUFdJa0p0ZDdYV1lQNll5MjJEK2tLN09oSApScTNDaVlNdERtWm91Yi9rVkJMME1WZFNtN2huMVRTVlRIakZvVzZjd1EzN2lLSGprWlZSd1gxS3p0MEIKLS0tLS1FTkQgUlNBIFBSSVZBVEUgS0VZLS0tLS0K",
					config.StakingCertContentKey:      "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZOekNDQXg4Q0NRQzY4N1hGeHREUlNqQU5CZ2txaGtpRzl3MEJBUXNGQURCL01Rc3dDUVlEVlFRR0V3SlYKVXpFTE1Ba0dBMVVFQ0F3Q1Rsa3hEekFOQmdOVkJBY01Ca2wwYUdGallURVFNQTRHQTFVRUNnd0hRWFpoYkdGaQpjekVPTUF3R0ExVUVDd3dGUjJWamEyOHhEREFLQmdOVkJBTU1BMkYyWVRFaU1DQUdDU3FHU0liM0RRRUpBUllUCmMzUmxjR2hsYmtCaGRtRnNZV0p6TG05eVp6QWdGdzB4T1RBM01ESXhOakV5TWpsYUdBOHpNREU1TURjeE1ERTIKTVRJeU9Wb3dPakVMTUFrR0ExVUVCaE1DVlZNeEN6QUpCZ05WQkFnTUFrNVpNUkF3RGdZRFZRUUtEQWRCZG1GcwpZV0p6TVF3d0NnWURWUVFEREFOaGRtRXdnZ0lpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElDRHdBd2dnSUtBb0lDCkFRRGdLNXI1dmRIdEpGRWd3N2hHRS9sekthSGN2d3pyMzJhcm1xMGs5dFljaEpYZlQzazFqMWxYdEJBZGNVTjMKZ1NSS2pnekgvdmpibjBlYTNBaURDVWQyTWNrL24wS2NKWjQzUzVJN1pqUDdyYmF2Mjk2YktDWjFIcjdyNWdYWQpGaGsrM2FVc1ZmRFVxQVBCd3lQOEtlVjMxQVJWQS9zK1dQZVdxczY5UVhUZHlKdUJZRTVwcjQwdjFTZitlYlVJCm5aMzd1R1kza2lPMEV4L0pnY29Rc0dKenJXRC96dGJSQ0ZJdnJkTkpaZDBwR3ZNbG1US3A3WHNNUjNjcHZxazcKNzAvL01MQ2R5R1cvMUlBclRTdUQxdmQ3bUJYMUp5VlhLeWNZTjB2SU90Ymd4UE9GdXRVeXFET2VQN281MXE0aQpQUzNkQ1JnZm1uL2hXTHd5K0N0SmUwQkdLc2I0dGswdEt4bzBzZTh2OUpBOG1VdG5tem1NdDRZOWppak9yQ09CCjdYd1dLbUpZRW04TjVVYmN5NmNwMm9MOHZRVnR6ejNQWHJrRnQrM2NGdDFqcmpkcFFZZ0g0anlra1dEZU9qRWYKeTFGQ3d6c05SdWRMVHZMaGZMbjg2L1pUNGNMWjlKSTcvV1cwSVBDOEZjN2xoem5KK2JJUVVlRW5kYUdkZ1ZreAp1RWcwTXhkck1yMGpVMElGb1h5U1JYTlJ6Y0RXWlNoRWpCVHY3dG5GeExtb05VK3VKYi9LcE1INnNSWWkzenM4CjVlY2FNS055RytMRG1CYWhVbEh4NWhLQUg0OU84ODU1K0FNaHNnOTFPTlpKbGRqUVgwb1pySUt6SzVCcHNxZVQKbDRjMll0L2ZBTGlaYWVGazFwQkVzdlZlTU9CQ0l1V0UrYjRVSUVhTEFPaHhmd0lEQVFBQk1BMEdDU3FHU0liMwpEUUVCQ3dVQUE0SUNBUUIrMlZYbnFScWZHN0gyL0swbGd6eFQrWDlyMXUrWURuMEVhVUdBRzcxczcwUW5xYnBuClg3dEJtQ0tMTjZYZ1BMMEhyTjkzM253aVlybWZiOFMzM3paN2t3OEdKRHZhVGFtTE55ZW00LzhxVEJRbW5Sd2UKNnJRN1NZMmw3M0lnODdtUjBXVGkrclRuVFR0YzY2Ky9qTHRGZWFqMFljbDloQlpYSEtpVUxTR2hzYlVid3RregppdU5sQU5ob05LWE5JQUJSSW1VcTZPd1loRVFOMER3SFhqNzl3a3B5RFlqS1p3SHVFWlVrbmM4UGwyb1FQQmtlCm1pbDN0c3J2R1Jrd2hpc25YWDd0cWg2cldLVlpOSmtPNjhoeTdYTzlhVFhqYmNCLzdZMUs4M0lTTkV5R1BzSC8KcHdGeWQvajhPNG1vZHdoN1Vsd3cxL2h3Y3FucWlFRkUzS3p4WDJwTWg3VnhlQW1YMnQ1ZVhGWk9sUngxbGVjTQpYUmtWdTE5bFlES1FIR1NyR3huZytCRmxTT0I5NmU1a1hJYnVJWEtwUEFBQ29CUS9KWllidEhrczlIOE90TllPClAyam9xbW5ROXdHa0U1Y28xSWkvL2oydHVvQ1JDcEs4Nm1tYlRseU5ZdksrMS9ra0tjc2FpaVdYTnJRc3JJRFoKQkZzMEZ3WDVnMjRPUDUrYnJ4VGxSWkUwMVI2U3Q4bFFqNElVd0FjSXpHOGZGbU1DV2FZYXZyQ1pUZVlhRWl5RgpBMFgyVkEvdlo3eDlENVA5WjVPYWtNaHJNVytoSlRZcnBIMXJtNktSN0IyNmlVMmtKUnhUWDd4UTlscmtzcWZCCjdsWCtxMGloZWVZQTRjSGJHSk5Xd1dnZCtGUXNLL1BUZWl5cjRyZnF1dHV0ZFdBMEl4b0xSYzNYRnc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
					config.StakingSignerKeyContentKey: "QXZhbGFuY2hlTG9jYWxOZXR3b3JrVmFsaWRhdG9yMDU=",
				},
			},
		},
	}
	for _, node := range network.Nodes {
		if err := node.EnsureNodeID(); err != nil {
			panic(fmt.Sprintf("failed to ensure nodeID: %s", err))
		}
	}

	return network
}