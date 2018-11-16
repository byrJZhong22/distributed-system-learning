---
title:      Google GFS学习     # 标题 
subtitle:   MIT 6.824               #副标题
date:       2018-10-21              # 时间
author:     ZJ                      # 作者
tags:                               #标签
    - 分布式
    - mit 6.824
---

## GFS的应用场景
在学习GFS的原理之前，我们首先应该了解GFS在设计时面对的主要需求场景。总的来说，GFS的设计主要是基于以下几个需求：
- 节点失效是常态，系统会构建在大量的普通机器上，这使得节点失效的可能性很大。因此，GFS必须要有较高的容错性(fault tolerance)、能够持续地监控自身的状态，同时还要能够迅速地从节点失效中快速恢复。
- 文件以大文件为主。系统需要存储的内容在通常情况下由数量不多的大文件构成。GB量级的文件在GFS非常常见，但是同时也应该支持小文件，但不需要为其作出优化。
- 文件主要以追加新数据为主而不是覆写已经存在的数据。文件的随机写操作在实际实现中并不存在。另外还包括大容量的连续读和小容量的随机读。
- 系统应当支持高效且原子的文件追加操作，保证多客户端同时对文件追加内容时不需要额外的同步操作。
- 大数据吞吐量比低延时更重要。

## GFS接口
GFS提供一种熟悉的文件系统接口，但不完全按照比如POSIX的标准API实现。文件在文件目录中有层次地组织在一起，并且通过路径名唯一标识。GFS支持常见的操作，包括create，delete，open，close，read和write。另外，GFS还支持snapshot和record append操作。snapshot在低开销的情况下创建文件或者目录树的一份拷贝。record append允许多个客户端同时向相同文件追加数据，并且保证每个客户端追加数据的原子性。在多路归并结果和生产者消费者队列的场景下方便多个客户端不需要显式加锁即可完成数据追加操作。

## GFS集群架构
GFS包括一个GFS master节点、若干个chunk server和若干个GFS用户GFS client，master和chunk server会作为用户进程运行在普通的linux机器上。整体架构如下图：

![](https://i.postimg.cc/ZY27rGdS/gfs-architecture.png)

存储文件时，GFS会把文件切分成若干个固定长度（fixed-size）的块（chunk）并存储。master在创建chunk时会为它们赋予一个全局唯一的不可变的64位Handle（句柄），并把它们移交给chunk server。chunk server则以普通的linux文件的形式将每个chunk存储在自己的本地磁盘上。为了确保可靠性，GFS会把每个chunk备份到多个chunk server中，默认为三份备份。

GFS的master节点负责保存整个文件系统的元数据，包括了命名空间、访问控制信息、文件到chunk的映射关系和chunk存放的位置。它同样控制全局的chunk lease管理、无用的chunk回收、chunk server之间迁移chunk。master节点周期性通过心跳信号和chunk servers进行通信，master节点借此向chunk server发送指令和收集chunk server的状态。

由于整个集群只有一个master，客户端在和GFS集群通信时，首先会从master处获取GFS的元数据，而实际文件的数据传输则会与chunk server直接进行，以避免master成为整个系统的数据传输瓶颈；除此以外，客户端也会在一定时间内缓存master返回的集群元数据。

## GFS的单个master设计
单个master的设计极大简化了整个系统的设计并且保证master能够利用全局的信息完成复杂的chunk放置操作和备份操作。但是，也需要最小化master在读取文件和写入文件过程中的参与程度，以避免它成为整个系统的瓶颈。client不通过master去读取文件和写入文件。client向master询问它应该访问哪一个chunk server，然后缓存这个信息一定的期限并直接与指定的chunk server交互完成后续的操作。

根据上面的架构图来分析一个简单的读取操作的过程。首先，根据固定的块大小，client可以根据文件名和字节偏移量获得要读取该文件的chunk索引，然后client向master发送一个包含文件名和chunk index的请求。master将相应的chunk句柄和备份的位置返回给client。client用文件名和chunk index作为key缓存master返回的信息。

然后client发送请求给其中一个chunk备份，一般来说是最近的一个。请求会指定chunk句柄和在这个chunk的字节偏移量。

## GFS的元数据
GFS集群的所有元数据都会保存在master的内存中。鉴于整个集群只会有一个master，这也使得元数据的管理变得更为简单。GFS集群的元数据主要包括以下三类信息：

- 文件与chunk的namespace
- 文件与chunk之间的映射关系
- 每个chunk replica所在的位置

前两类元数据也会通过log操作持久化到master的本地磁盘和备份到远程机器上。

元数据保存在master的内存中使得master要对元数据作出变更变得极为容易；同时，这也使得master能够更加高效地扫描集群的元数据，以唤起chunk进行回收、chunk均衡等系统级管理操作。唯一的不足在于这使得整个集群所能拥有的chunk数量受限于master的内存大小，但是在实践中并不是一个严重的限制，master只需要为每个64 MB的chunk维持一份大小小于64 byte的元数据。

为了保证元数据的可用性，master在堆元数据做任何操作之前都会采用先写日志的形式将操作进行记录，日志写入完成后再进行实际操作，而这些日志也会被备份到多个机器上进行保存。不过，chunk replica的位置不会被持久化到日志中，而是由master在启动时询问各个chunk server当前所有的replica。这可以省去master与chunk server同步数据的成本，同时进一步简化master日志持久化的工作。

`operation log`，log 操作包含了关键元数据变化的历史记录，它对于 GFS 非常关键。它不仅仅持久化元数据的记录，也可以作为逻辑时间线，决定并发操作的先后次序。文件和 chunk 的版本 version 在创建时由 logical time 唯一指定的。

## 一致性模型

GFS 有一个松弛的一致性模型，用于支持我们完成一个实现相对简单和高效的分布式应用。

首先，命名空间完全由单节点 master 管理在其内存中，这部分数据的修改需要确保原子性，实现上可以通过让 master 添加互斥锁来解决并发修改的问题。

文件的数据修改则相对复杂。首先我们先明确，在文件额某一部分被修改后，它可能进入以下三种状态的其中之一：

- 客户端读取不同的 replica 时可能会读取到不同的内容，那这部分文件是`不一致`的(inconsitent)。
- 所有客户端无论读取哪一个 replica 都会读取到相同的内容，那这部分文件是`一致`的(consistent)。
- 所有客户端都能看到上一次修改的所有完整内容，且这部分文件是一致的，那么我们说这部分文件是`确定`的(defined)。(个人对defined的理解：defined包含了consistent，即如果一个chunk是defined，就一定是consistent的；其次，用户能够看到每一次mutation实际进行了什么操作写了什么数据)

在修改后，一个文件的当前状态取决于此次修改的类型以及修改是否成功，具体来说：

- 如果一次写入操作成功且没有与其他并发的写入操作发生重叠，那么这部分的文件是`确定`的（同时也是一致的）。
- 如果有若干个写入操作并发地执行成功，那么这部分文件会是`一致`的但会是`不确定`的，在这种情况下，客户端所能看到的数据通常不能直接体现在其中的任何一次修改。(因为在并发的情况下，就有可能导致用户采用它觉得合理的offset，而实际上会导致并发写入的数据相互混合，这样，我们就无从得知这一堆混合的数据里，都是哪些操作分别写入了哪部分数据。但是在读的时候，又确实是相同的结果，此为 undefined but consistent)
- 失败的写入操作会让文件进入`不一致`的状态。

论文整理了之间的关系表格：

![](https://i.postimg.cc/1tpvDMw0/gfs-file-state.png)

GFS支持的Data Mutation包括两种：指定偏移值的数据写入(Write)和数据追加(Record Append)。当写入时，指定的数据会被直接写入到客户端指定的偏移位置中，覆盖原有的数据。串行写入成功时，文件状态为defined。GFS并未为该操作提供太多的一致性保证；如果不同的客户端并发地写入同一块文件区域，操作完成后这块区域的数据可能由各次写入的数据碎片所组成，即进入`不确定`的状态。无论是串行写入还是并发写入，写入失败则会处于inconsistent状态。

与写入操作不同，GFS确保即使是在并发的情况下，数据追加操作也是原子且at least once（至少一次）的。操作完成后，GFS会把实际写入的偏移值返回给客户端，该偏移值即代表包含所写入数据的`确定`的文件区域的起始位置。
由于数据追加操作是at least once，GFS有可能会在文件中写入填充(padding)或是重复数据，但出现的概率不高。

在读取数据时，为了避免读入填充数据或是损坏的数据，数据在写入前往往会放入一些如校验和等元信息以用于验证其可用性，GFS的客户端便可以在读取时自动跳过填充和损坏的数据。不过，鉴于数据追加操作的at least once特性，客户端仍有可能读入重复的数据，此时只能由上层应用通过鉴别记录的唯一ID等信息过滤重复数据。

### 对上层应用的影响

GFS的一致性模型是相对松弛的，这就要求上层应用在使用GFS时能够适应GFS所提供的一致性语义。简单来说，上层应用可以通过两种方式来做到这一点：更多地使用追加操作而不是覆写操作；写入包含校验信息的数据。

更多地使用追加操作而不是覆写操作的原因是：GFS 针对追加操作做出了显著的优化，这使得这种数据写入方式的性能更高，而且也能提供更强的一致性语义。尽管如此，追加操作 at least once 的特性仍使得客户端可能读取到填充或是重复数据，这要求客户端能够容忍这部分无效数据。一种可行的做法是在写入的同时为所有记录写入各自的校验和，并在读取时进行校验，以剔除无效的数据；如果客户端无法容忍重复数据，客户端也可以在写入时为每条记录写入唯一的标识符，以便在读取时通过标识符去除重复的数据。

## GFS的交互流程

### leases和mutation order

所有chunk数据修改操作都需要在所有 replica 执行。GFS 使用 lease 维持所有 replica 的一致性修改次序。master 给其中一个 replica 分配一个被称为 primary 的 chunk lease。primary 才有权针对 chunk 选择一个数据修改顺序。所有 replica 都按照这个顺序执行数据修改。lease 机制用于最小化 master 的管理成本。

通过对下图的写文件过程阐述，可以分析 lease 机制的具体过程：

![](https://i.postimg.cc/yx16kxCy/gfs-data-flow.png)

1. 客户端向master询问是否存在一个chunk server正在持有目标chunk的租约和其他replica的位置。如果不存在这样的chunk server，master会选择一个replica并授予租约(lease)；
2. master向客户端回复作为primary的chunk replica和其他replica的位置。client将这些信息缓存起来方便后面的调用。当primary不可达或者不再持有一个租约(lease)时，客户端需要重新与master通信。
3. 客户端将需要写入的数据推送给所有replica。replica都是存储在chunk server。chunk server使用一个LRU cache存储这个数据，知道数据被使用或者过期。通过这种方式，把data flow和control flow进行解耦，可以提高整体性能。(即客户端先向 Chunk Server 提交数据，再将写请求发往 Primary。这么做的好处在于 GFS 能够更好地利用网络带宽资源。)推送数据的具体方法是client只需要push数据给closest chunk server，chunk server之间采用pipelined fashion传递数据。
4. 当所有replica都通知已经收到数据，客户端发送一次写请求给primary replica。primary对收到的数据修改操作分配连续的序列号，这些数据修改操作有可能来自多个客户端。
5. primary将写请求转发给剩下的secondary replicas。每一个secondary replica都按照primary指定的序列号顺序进行mutation操作。
6. secondary replicas向primary回复完成操作。
7. primary向客户端回复。如果存在发生错误的replica也会被反馈给客户端。如果是primary出现错误，不会分配serial number和转发给其他replicas，如果是部分replicas出现错误，客户端会认为写入请求失败，并且被修改的区域设置为`inconsistent`状态。

### Atomic Record Appends

文件追加操作的过程和写入的过程有几分相似：

1. 客户端将数据推送到每个 replica，然后将请求发往primary
2. primary首先判断将数据追加到该块后是否会令块的大小超过上限：如果是，那么primary会为该块写入填充至其大小达到上限，并通知其他replica执行相同的操作，再响应客户端，通知其应在下一个块上重试该操作
3. 如果数据能够被放入到当前块中，那么primary会把数据追加到自己的 replica中，拿到追加成功返回的偏移值，然后通知其他replica将数据写入到该偏移位置中
4. 最后primary再响应客户端

当追加操作在部分replica上执行失败时，primary会响应客户端，通知它此次操作已失败，客户端便会重试该操作。和写入操作的情形相同，此时已有部分 replica顺利写入这些数据，重新进行数据追加便会导致这一部分的replica上出现重复数据，不过GFS的一致性模型也确实并未保证每个replica都会是完全一致(bytewise identical)的。它只会保证数据至少被写入一次(at least once)。

GFS只确保数据会以一个原子的整体被追加到文件中至少一次。由此我们可以得出，当追加操作成功时，数据必然已被写入到所有replica的相同偏移位置上，且每个replica的长度都至少超出此次追加的记录的尾部，下一次的追加操作必然会被分配一个比该值更大的偏移值，或是被分配到另一个新的块上。

### Snapshot

GFS 还提供了文件快照操作，可为指定的文件或目录创建一个副本。

快照操作的实现采用了写时复制(Copy on Write)的思想：

1. 在master接收到快照请求后，它首先会撤销这些chunk的lease，以让接下来其他客户端对这些chunk进行写入时都会需要请求master获知primary的位置，master便可利用这个机会创建新的chunk。
2. 当chunk lease撤销或失效后，master会先写入日志，然后对自己管理的命名空间进行复制操作，复制产生的新记录指向原本的chunk。
3. 当有客户端尝试对这些chunk进行写入时，master会注意到这个chunk的引用计数大于1。此时，master会为即将产生的新chunk生成一个handle，然后通知所有持有这些chunk的chunk server在本地复制出一个新的chunk，分配新的handle，然后再返回给客户端。

## Replica管理

为了进一步优化GFS集群的效率，master在replica的位置选取上会采取一定的策略。

master的replica编排策略主要为了实现两个目标：最大化数据的可用性，以及最大化网络带宽的利用率。为此，replica不仅需要被保存在不同的机器上，还会需要被保存在不同的机架上，这样如果整个机架不可用了，数据仍然得以存活。如此一来，不同客户端对同一个chunk进行读取时便可以利用上不同机架的出口带宽，但坏处就是进行写入时数据也会需要在不同机架间流转，不过在GFS的设计者看来这是个合理的trade-off。

replica的生命周期转换操作实际只有两个：创建和删除。首先，replica的创建可能源于以下三种事件：创建chunk、为chunk重备份、以及replica均衡。

在master创建一个新的chunk时，首先它会需要考虑在哪放置新的replica。master会考虑如下几个因素：

1. master会倾向于把新的replica放在磁盘使用率较低的chunk server。
2. master会倾向于确保每个chunk server上"较新"的replica不会太多。因为新chunk的创建意味着接下来会有大量的写入，如果某些chunk server上有太多的新chunk replica，那么写操作压力就会几种在这些chunk server上。
3. 如上文所述，master会倾向于把replica放在不同的机架上。

当某个chunk的replica数量低于用户指定的阈值时，master就会对该chunk进行重备份。这可能是由chunk server失效、chunk server报告replica数据损坏或者是用户提高了replica数量阈值所触发。

首先，master会按照一下因素为每个需要重备份的chunk安排优先级：

1. 该chunk的replica数与用户指定的replica数量阈值的差距
2. 优先为未删除的文件（具体见下文）的chunk进行重备份
3. 除此之外，master还会提高任何正在阻塞用户操作的chunk的优先级

有了chunk的优先级后，master会选取当前拥有最高优先级的chunk，并指定若干chunk server直接从现在已有的replica上复制数据。master具体会指定哪些chunk server进行复制操作同样会考虑上面提高的几个因素。另外，为了减少重备份对用户使用的影响，master会限制当前整个集群正在进行的复制操作的数量，同时chunk server也会限制复制操作所使用的带宽。

最后，master会周期地检查每个chunk当前在集群内的分布情况，并在必要时迁移部分replica以更好地均衡各节点的磁盘利用率和负载。新replica的位置选取策略和上面提到的大体相同，另外master还会需要选择要移除哪个已有的replica，简单概括就是master会倾向于移除磁盘占用率较高的chunk server上的replica，以均衡磁盘利用率。

### 文件删除

当用户对某个文件进行删除时，GFS不会立刻删除数据，而是在文件和chunk两个层面上都惰性地对数据进行移除。

首先，当用户删除某个文件时，GFS不会从namespace中直接移除该文件的记录，而是把文件重命名为另一个隐藏的名称，并带上删除时的时间戳。当master周期扫描namespace时，它会发现那些已经被删除较长时间，比如三天，的文件，这时候，master才会真正地将其从namespace中移除。在文件被彻底从namespace删除前，客户端仍然可以利用这个重命名后的隐藏名称读取该文件，甚至再次将其重命名以撤销该删除操作。

master在元数据中维持文件与chunk之间的映射关系：当namespace中的文件被移除后，对应chunk的引用计数便自动减1.同样是在master周期扫描元数据的过程中，master会发现引用计数已为0的chunk，此时master便会从自己的内存中移除与这些chunk有关的元数据。在chunk server和master进行的周期心跳通信过程中，chunk server会汇报自己所持有的chunk replica，此时master便会告知chunk server哪些chunk已不存在于元数据中，chunk server则可以自行移除对应的replica。

采用这种删除机制主要有如下三点好处：

1. 对于大规模的分布式系统来说，这样的机制更为可靠：在chunk创建时，创建操作可能在某些chunk server上成功了，在其他chunk server上失败了，这导致某些chunk server上可能存在master 不知道的replica。除此以外，删除replica的请求可能会发送失败，master会需要记得尝试重发。相比之下，由chunk server主动地删除replica能够以一种更为统一的方式解决以上的问题
2. 这样的删除机制将存储回收过程与master日常的周期扫描过程合并在了一起，这就使得这些操作可以以批的形式进行处理，以减少资源损耗；除外，这样也得以让master选择在相对空闲的时候进行这些操作
3. 用户发送删除请求和数据被实际删除之间的延迟也有效避免了用户误操作的问题

不过，如果在存储资源较为稀缺的情况下，用户对存储空间使用的调优可能就会受到该机制的阻碍。为此，GFS允许客户端再次指定删除该文件，以确实地从namespace层移除该文件。除外，GFS还可以让用户为namespace中不同的区域指定不同的备份和删除策略，如限制GFS不对某个目录下的文件进行chunk备份等。

## 高可用机制

### master

前面总结到master会以先写日志(Operation log)的形式对集群元数据进行持久化：日志在被确实写出之前，master不会对客户端的请求进行响应，后续的变更便不会执行；此外，日志还会被备份到其他的多个机器上，日志只有在写入到本地以及远端备份的持久化存储中才被视为完成写出。

在重新启动时，master会通过重放已保存的操作记录来恢复自身的状态。为了保证master能够快速地完成恢复，master会在日志达到一定大小后为自身的当前状态创建checkpoint，并删除checkpoint创建之前的日志，重启时便从最近一次创建的checkpoint开始恢复。checkpoint文件的内容会以B树的形式进行组织，且在被映射到内存后便能够在不做其他额外的解析操作的情况下检索其所存储的namespace，这便进一步减少了master恢复所需的时间。

为了简化设计，同一时间只有一个master起作用，当master失效时，外部的监控系统会检测到这一事件，并在其他地方重新启动新的master进程。

此外，集群中还会有其他提供只读功能的shadow master：它们会同步master的状态变更，但有可能延迟若干秒，其主要用于为master分担读操作的压力。shadow master会通过读取master操作日志的某个备份来让自己的状态与master同步；它也会像master那样在启动时轮询各个chunk server，获知它们锁持有的chunk replica信息，并持续监控它们的状态。实际上，在master失效后，shadow master仍能为整个GFS集群提供只读功能，而shadow master对master的依赖只限于replica位置的更新事件。

### chunk server

作为集群的slave角色，chunk server失效的几率比master要大得多。在前面已经提到，chunk server失效时，其所持有的replica对应的chunk的replica数量便会减少，master会发现replica数量低于用户指定阈值并安排重备份。

此外，当chunk server失效时，用户的写入操作还会不断地进行，那么当chunk server重启后，chunk server上的replica数据有可能是已经过期的。为此，master会为每个chunk维持一个版本号，以区分正常的和过期的replica。每当master将chunk lease分配给一个chunk server时，master会提高chunk的版本号，并通知其他最新的replica更新自己的版本号。如果此时有chunk server失效了，那么它上面的replica的版本号不会发生变化。

在chunk server重启时，chunk serve会向master汇报自助机所持有的chunk server及对应的版本号。如果master发现某个replica版本号过低，便会认为这个replica不存在，如此一来这个过期的replica便会在下一次的replica回收过程中被移除。此外，master向客户端返回replica位置信息时也会返回chunk当前的版本号，如此一来客户端便不会读取到旧的数据。

### 快速重启

master和chunk server都被设计为保存状态和在秒级时间内重启。

## 数据完整性

如前面所述，每个chunk都会以replica的形式被备份在不同的chunk server中，而且用户可以为namespace的不同部分赋予不同的备份策略。

为了保证数据完整，每个chunk server都会以校验和的形式来检测自己保存的数据是否有损坏；在侦测到损坏数据后，chunk server也可以利用其它replica来恢复数据。

首先，chunk server会把每个chunk replica切分为若干个64KB大小的块，并为每个块计算32位校验和。和master的元数据一样，这些校验和会被保存在chunk server的内存中，每次修改前都会用先写日志的形式来保证可用。当chunk server接收到读请求时，chunk server首先会利用校验和检查所需读取的数据是否有发生损坏，如此一来chunk server便不会把损坏的数据传递给其他请求发送者，无论它是客户端还是另一个chunk server。发现损坏后，chunk server会为请求发送者发送一个错误，并向master告知数据损坏事件。接收到错误后，请求发送者会选择另一个chunk server重新发起请求，而master则会利用另一个replica为该chunk进行重备份。当新的replica创建完成后，master便会通知该chunk server删除这个损坏的replica。

当进行数据追加操作时，chunk server可以为位于chunk尾部的校验和块的校验和进行增量式的更新，或是在产生了新的校验和块时为其计算新的校验和。即使是被追加的校验和块在之前已经发生了数据损坏，增量更新后的校验和依然会无法与实际的数据相匹配，在下一次读取时依然能够检测到数据的损坏。在进行数据写入操作时，chunk server必须读取并校验包含写入范围起始点和结束点的校验和块，然后进行写入，最后再重新计算校验和。

除外，在空闲的时候，chunk server也会周期地扫描并校验不活跃的chunk replica的数据，以确保某些chunk replica即使在不怎么被读取的情况下，其数据的损坏依然能被检测到，同时也确保了这些已损坏的chunk replica不至于让master认为该chunk已有足够数量的replica。


## FAQ

MIT 6.824针对这篇论文给出了一些相关的FAQ：

- Why is atomic record append at-least-once, rather than exactly once?

要做到追加操作是确定一次是非常困难的，因为需要primary保存一些状态信息以检测重复的数据，而这些信息也需要复制到其他服务器上，以确保primary失效时这些信息不会丢失。

- How does an application know what sections of a chunk consist of padding and duplicate records?

如果需要检测数据填充，上层应用可以在每个有效记录之前加上一个magic number进行标记，或者用校验和保证数据的有效性。上层应用可以通过在记录中添加唯一的ID来检测重复数据，这样应用在读入数据时就可以利用已经读入的ID来排除重复的数据了。

- How can clients find their data given that atomic record append writes it at an unpredictable offset in the file?

追加操作（以及GFS本身）主要是面向那些会完整读取文件的应用的。这些应用会读取所有的记录，所以它们并不需要提前知道记录的位置。例如，一个文件中可能包含若干个并行的网络爬虫获取的所有链接URL。这些URL在文件中的偏移值是不重要的，应用只会想要完整读取所有URL。

- The paper mentions reference counts -- what are they?

引用计数是snapshot的copy-on-write机制实现的一部分。当GFS创建一个snapshot时，不会立即复制chunk，而是增加每一个chunk的引用计数。这样保证创建snapshot操作的开销低。当客户端向chunk写入数据并且master发现chunk的引用计数大于1，master会首先创建一份拷贝以便客户端更新拷贝。

- If an application uses the standard POSIX file APIs, would it need to be modified in order to use GFS?

答案是需要的，不过 GFS 并不是设计给已有的应用的，它主要面向的是新开发的应用，如 MapReduce 程序。

- How does GFS determine the location of the nearest replica?

论文中有提到GFS是基于保存replica的服务器的IP地址来判断距离的。在2003年的时候，Google分配IP地址的方式应该确保了如果两个服务器的IP地址在IP地址空间中较为接近，那么它们在机房中的位置也会较为接近。

- Does Google still use GFS?

Google仍然在使用GFS，而且是作为其他如BigTable等存储系统的后端。由于工作负载的扩大以及技术的革新，GFS的设计在这些年里无疑已经经过大量调整了，但我并不了解其细节。HDFS是公众可用的对GFS的设计的一种效仿，很多公司都在使用它。

- Won't the master be a performance bottleneck?

确实有这个可能，GFS的设计者也花了很多心思来避免这个问题。例如，master会把它的状态保存在内存中以快速地进行响应。从实验数据来看，对于大文件读取（GFS主要针对的负载类型），master不是瓶颈所在；对于小文件操作以及目录操作，master 的性能也还跟得上（见论文6.2.4节）。

- How acceptable is it that GFS trades correctness for performance and simplicity?

这是分布式系统领域的老问题了。保证强一致性通常需要更加复杂且需要机器间进行更多通信的协议（正如我们会在接下来几门课中看到的那样）。通过利用某些类型的应用可以容忍较为松弛的一致性的事实，人们就能够设计出拥有良好性能以及足够的一致性的系统。例如，GFS对MapReduce应用做出了特殊优化，这些应用需要的是对大文件的高读取效率，还能够容忍文件中存在数据空洞、重复记录或是不一致的读取结果；另一方面，GFS则不适用于存储银行账号的存款信息。

- What if the master fails?

GFS集群中会有持有master状态完整备份的replica master；通过论文中没有提到的某个机制，GFS会在master失效时切换到其中一个replica（见 5.1.3 节）。有可能这会需要一个人类管理者的介入来指定一个新的master。无论如何，我们都可以确定集群中潜伏着一个故障单点，理论上能够让集群无法从master失效中进行自动恢复。我们会在后面的课程中学习如何使用Raft协议实现可容错的Master。

## 总结

从设计上看，在强一致性面前，GFS选择了更高的吞吐性能以及自身架构的简洁。高性能和强一致性之间的矛盾是分布式系统领域经久不衰的话题，它们通常是不可兼得的。为了实现理想的一致性，系统也可能面临来自并发操作、机器失效、网咯隔离等问题所带来的挑战。