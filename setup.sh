sudo groupadd libvirt
sudo usermod -a -G libvirt user

sudo touch /etc/udev/rules.d/10-vfio-permissions.rules
sudo echo `SUBSYSTEM=="vfio" OWNER="root" GROUP="libvirt" MODE="0660"` >> /etc/udev/rules.d/10-vfio-permissions.rules
sudo echo `KERNEL=="vsock" OWNER="root" GROUP="libvirt" MODE="0660"`   >> /etc/udev/rules.d/10-vfio-permissions.rules

