---
title:      Google MapReduce总结     # 标题 
subtitle:   MIT 6.824               #副标题
date:       2018-10-21              # 时间
author:     ZJ                      # 作者
tags:                               #标签
    - 分布式
    - mit 6.824
---
## 概要
这篇文章是本人在学习MIT 6.824的Google MapReduce并完成对应Lab的基础上总结而成。本文会详细介绍Google MapReduce的原理。

## Google MapReduce学习总结 

### MapReduce编程模型

MapReduce所执行的分布式计算会以一组键值对作为输入，输出另一组键值对，用户通过编写Map函数和Reduce函数来指定所要进行的计算。

由用户编写的Map函数将被应用在每一个输入键值对，并输出若干键值对作为中间结果。之后，MapReduce框架则会将与同一个键值I关联的值都传递给同一个Reduce函数调用中。

同样由用户编写的Reduce函数以键值I与键值I相关联的Values集合作为参数，对传入的值进行合并和输出合并后的值的集合。

形式化地说，由用户提供的Map函数和Reduce函数应该是如下形式：

```
map(k_1, v_1) -> list(k_2, v_2)
reduce(k_2, list(v_2)) -> list(v_2)
```

在实际实现中，MapReduce框架使用`Iterator`来作为输入的集合，避免集合过大，无法被完整地放入内存中。

考虑一个实际案例，我们考虑这样一个问题：给定大量的文档，计算其中每一个单词出现的次数。伪代码实现如下：

```
map(String key, String value):
  // key: document name
  // value: document contents
  for each word w in value:
    EmitIntermediate(w, “1”);


reduce(String key, Iterator values):
  // key: a word
  // values: a list of counts
  int result = 0;
  for each v in values:
    result += ParseInt(v);
  Emit(AsString(result));
```

### MapReduce的计算过程
每一轮MapReduce的大致过程如下：

![](https://i.postimg.cc/L8TpsZqw/mapreduce-architecture.png)

用户首先在客户端指定Map函数和Reduce函数，以及此次MapReduce计算的配置，包括中间结果键值对的Partition数量R以及用于切分中间结果的哈希函数hash。用户开始 MapReduce计算后，整个MapReduce计算的流程可总结如下：

1. 作为输入的文件会被分为M个split，每个split的大小通常为16~64Mb之间。
2. 然后，整个MapReduce计算包含M个map任务和R个Reduce任务。Master节点会从空闲的Worker节点中选取并为其分配map任务和reduce任务。
3. 收到Map任务的Worker（称其为Mapper）开始读入自己对应的split，将读入的内容解析为输入键值对并调用用户自定义的Map函数。由Map函数产生的中间结果键值对会被暂时存放在缓冲内存区中。
4. 在Map阶段进行的时候，负责完成map任务的Worker（Mapper）周期性地将放置在缓冲区的中间结果存入到自己的本地磁盘中，同时根据用户指定的Partition函数（默认为hash(key) mod R）将产生的中间结果分为R个部分、任务完成时，Mapper会将中间结果的在其本地磁盘存放的位置报告给Master。
5. Mapper上报的中间结果的位置会被Master转发给Reducer。当Reducer接收到这些信息后便会通过RPC读取存储在Mapper本地磁盘上属于Partition的中间结果。在读取完毕后，Reducer会对读取到的数据进行排序以令拥有相同键的键值对能够连续分布。
6. 之后，Reducer为每个键手机与其关联的值的集合，并以键和集合调用用户定义的Reduce函数。Reduce函数的结果会被放入到对应的Reduce Partition结果文件。

### Master的数据结构

在一个MapReduce集群中，Master会记录每一个Map和Reduce任务的当前完成状态（idle，in-progress，or completed），以及任务分配的Worker。除此之外，Master记录Mapper产生的中间结果文件的位置和大小，并转发给Reducer。

### MapReduce容错机制

#### Worker失效
在MapReduce集群中，Master会周期性地ping所有Worker。如果Worker在一定时间内没有发送响应，Master会认为该Worker已经失效，不可用。该Worker已完成的所有map任务都会被重新设置为初始状态并重新分配给其他Worker。同样地，正在进行的map任务和reduce任务都会设置为idle状态并可以重新分配给新的Worker。

如果有Reduce任务分配给该Worker，无论是正在运行还是已经完成，都需要由Master重新分配给其他的Worker，因为该Worker不可用也意味着存储在该Worker本地磁盘的中间结果也不可用。Master也会将这次重试通知给所有Reducer，没能从原本的Mapper上完整地获取中间结果的Reducer便会开始从新的Mapper上获取数据。

#### Master失效

整个 MapReduce 集群中只会有一个 Master 结点。

Master 结点在运行时会周期性地将集群的当前状态作为保存点（Checkpoint）写入到磁盘中。Master 进程终止后，重新启动的 Master 进程即可利用存储在磁盘中的数据恢复到上一次保存点的状态。

#### Straggler

如果集群中有某个Worker花了特别长的时间来完成最后几个map任务或者reduce任务，整个MapReduce流程的耗时会因此被拉长，这样的Worker被称为Straggler。产生Straggler的原因有很多，比如机器的磁盘性能差，MapReduce计划系统分配其他任务到相同机器上执行造成资源竞争，包括CPU，内存，本地磁盘和网络I/O。

MapReduce 在整个计算完成到一定程度时就会将剩余的任务进行备份，即同时将其分配给其他空闲Worker来执行，并在其中一个Worker完成后将该任务视作已完成。

### 其他优化

#### 数据本地性

在Google内部使用的计算环境中，机器间的网络带宽是相对稀缺的资源。GFS将每一个文件划分为多个64MB的块，并为每一个块保存三份副本在不同的机器上。Master在分配map任务时会从GFS读取每个Block的位置信息，并尽量将对应的map任务分配到持有该Block的Replica的机器上；如果无法将任务分配至该机器，Master也会利用GFS提供的集群的拓扑信息，尽可能将任务分配给与拥有文件副本的机器在同一个局域网下的其他机器。


#### Combiner

在某些情形下，用户所定义的Map任务可能会产生大量重复的中间结果键，同时用户所定义的Reduce函数本身也是满足交换律和结合律的。

在这种情况下，Google MapReduce系统允许用户声明在Mapper上执行的Combiner函数：Mapper会使用由自己输出的R个中间结果Partition调用Combiner函数以对中间结果进行局部合并，减少Mapper和Reducer间需要传输的数据量。