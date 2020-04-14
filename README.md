# vcable 
Virtual cable to avoid the requirement of bridges and provide a way of having
direct VM-to-VM networking that is encrypted and inaccessible from the host. 

### Experiment
Currently experimenting with vsock, possibly executed using memory FD library we
are managing. If this does not work we will consider a different virtio based
connection connected to a network device that handles encryption between VMs. 

### Functional Requirements
A few functional requirements, which is why this is still experimental:
  
  * **Must not be accesssible from Host or Hypervisor** so that it remains
    secret and only known to the VMs. 

  * **Must not pass through the host** ideally, it should write from reserved
    space from on VM directly to the reserved space of another to reduce
    latency. 

  * **Using memory and zerocopy transfers/connections to maximize connection**

  * **Should present a typical network interface, have an IP, and then the VM
    can serve only the data it wants the Controller VM to have over an API. 
