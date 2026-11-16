# Regional WAN exceptions

The panel supports Russian, English, Persian (RTL), Simplified Chinese and
Traditional Chinese. Changing language does not enable any routing rules.

For local services to use the ordinary WAN while other traffic uses VLESS,
edit the routing policy, keep VLESS as its primary path and select the relevant
cards under WAN exceptions. Save and Apply the change.

## Iran

The IR tab includes **Iranian domains and banks** (`ir-local`). This optional
card matches the `.ir` and `.ایران` domain suffixes and includes the existing
`ir-banks` catalog. The suffix rule is bundled with the image; the bank list
retains its regular catalog updates. It also covers bank domains outside those
national suffixes when they occur in the maintained bank list.

The separate bank, payment, government and other Iranian service cards remain
available. Enable **GeoIP IR** separately if Iranian IP ranges should also use
the WAN. Domain matching and IP matching are different rules.

## China / CN Direct

The CN tab contains Chinese service cards and a separate **GeoIP CN** option.
Select the service cards for domain matching and GeoIP CN for Chinese IP ranges.
Under WAN exceptions these rules use the direct WAN; their DNS path follows
the same routing decision. This uses the gateway's existing GeoIP support.

## Scope

Cards follow their routing policy. In the opposite mode, where WAN is primary
and exceptions use VLESS, selected exception cards use VLESS. No regional card
overrides that choice or silently changes existing policies.

Domain cards need a hostname available to routing. GeoIP uses destination IPs
and does not imply a domain suffix. Neither rule changes the policy for other
devices. A direct WAN exception uses the ordinary WAN address.
